package pulumicli

import (
	"encoding/json"
	"errors"
	"flag"
	r "github.com/lambdawalker/go.infra.composer/reporting"
	"io"
	"os"
	"path/filepath"
	"time"
)

// RunCLI renders a native offline catalog, optionally enriched from scoped cache,
// stack-export JSON and preview event JSONL. It never executes Pulumi or contacts a provider.
// --scope requires a JSON-encoded reporting.Scope; --feature filters exact ownership.
func RunCLI(args []string, catalog []r.Artifact, stdout io.Writer) error {
	fs := flag.NewFlagSet("artifact-report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scopeFile := fs.String("scope", "", "scope JSON file")
	cacheDir := fs.String("cache-dir", "", "scoped snapshot directory")
	outputDir := fs.String("output-dir", "", "write artifacts.md/json/html")
	state := fs.String("state", "", "Pulumi stack export JSON")
	preview := fs.String("preview-events", "", "Pulumi preview event JSONL")
	feature := fs.String("feature", "", "canonical feature selector")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected report arguments")
	}
	if *scopeFile == "" {
		return errors.New("--scope JSON file required")
	}
	b, e := os.ReadFile(*scopeFile)
	if e != nil {
		return errors.New("cannot read scope file")
	}
	var scope r.Scope
	if json.Unmarshal(b, &scope) != nil {
		return errors.New("invalid scope file")
	}
	report := r.Report{Scope: scope, Artifacts: append([]r.Artifact(nil), catalog...), Metadata: r.Metadata{Operation: "preview", Outcome: "planned"}}
	if *state != "" {
		f, err := os.Open(*state)
		if err != nil {
			report.Warnings = append(report.Warnings, "state_unavailable")
		} else {
			as, err := ReadState(f, time.Now().UTC())
			f.Close()
			if err != nil {
				report.Warnings = append(report.Warnings, "state_incomplete")
			}
			by := map[string]int{}
			for i, a := range report.Artifacts {
				by[a.Key] = i
			}
			for _, a := range as {
				if i, ok := by[a.Key]; ok {
					a.Owner = report.Artifacts[i].Owner
					if a.Feature == "" {
						a.Feature = report.Artifacts[i].Feature
					}
					report.Artifacts[i] = a
				} else {
					report.Artifacts = append(report.Artifacts, a)
				}
			}
		}
	}
	if *cacheDir != "" {
		cached, err := r.LoadCache(r.CachePath(*cacheDir, scope), scope)
		if err == nil {
			report = r.Merge(report, cached)
		} else if !os.IsNotExist(err) {
			report.Warnings = append(report.Warnings, "cache_unavailable")
		}
	}
	if *preview != "" {
		f, err := os.Open(*preview)
		if err != nil {
			report.Warnings = append(report.Warnings, "preview_unavailable")
		} else {
			report.Artifacts, err = ApplyPreview(report.Artifacts, f)
			f.Close()
			if err != nil {
				report.Warnings = append(report.Warnings, "preview_incomplete")
			}
		}
	}
	report.Artifacts = r.SelectFeature(report.Artifacts, *feature)
	if *outputDir != "" {
		if e = os.MkdirAll(*outputDir, 0700); e != nil {
			return e
		}
		for _, format := range []struct {
			ext   string
			write func(io.Writer, r.Report) error
		}{{"md", r.WriteMarkdown}, {"json", r.WriteJSON}, {"html", r.WriteHTML}} {
			f, err := os.OpenFile(filepath.Join(*outputDir, "artifacts."+format.ext), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			err = format.write(f, report)
			ce := f.Close()
			if err != nil {
				return err
			}
			if ce != nil {
				return ce
			}
		}
	}
	return r.WriteMarkdown(stdout, report)
}
