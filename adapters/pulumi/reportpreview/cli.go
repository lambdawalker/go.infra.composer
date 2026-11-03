package reportpreview

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

// Reconcile accepts an explicitly selected app snapshot only with matching identity.
// Ambiguous names and missing physical identities never supply evidence.
func Reconcile(current reporting.Report, cached reporting.Report, feature string) (reporting.Report, error) {
	current.Artifacts = append([]reporting.Artifact(nil), current.Artifacts...)
	if cached.SchemaVersion != reporting.SchemaVersion || current.Scope.Environment == "" || cached.Scope.Environment != current.Scope.Environment {
		return current, fmt.Errorf("cache environment or schema mismatch")
	}
	for _, p := range [][2]string{{current.Scope.Project, cached.Scope.Project}, {current.Scope.Account, cached.Scope.Account}, {current.Scope.Backend, cached.Scope.Backend}, {current.Scope.Region, cached.Scope.Region}, {current.Scope.Application, cached.Scope.Application}, {current.Scope.Stack, cached.Scope.Stack}} {
		if p[0] != "" && p[0] != p[1] {
			return current, fmt.Errorf("cache scope mismatch")
		}
	}
	// Project/account/backend are required to bind independently generated catalogs to app evidence.
	if current.Scope.Project == "" || current.Scope.Account == "" || current.Scope.Backend == "" {
		return current, fmt.Errorf("cache requires project, account and backend")
	}
	key := func(a reporting.Artifact) string {
		n := a.Key
		if j := strings.LastIndex(n, "::"); j >= 0 {
			n = n[j+2:]
		} else {
			n = a.Name
		}
		return a.Type + "\x00" + n
	}
	matches := map[string][]reporting.Artifact{}
	for _, a := range cached.Artifacts {
		if a.Feature == feature {
			matches[key(a)] = append(matches[key(a)], a)
		}
	}
	if len(matches) == 0 {
		return current, fmt.Errorf("cache has no matching feature")
	}
	counts := map[string]int{}
	for _, a := range current.Artifacts {
		counts[key(a)]++
	}
	for i, a := range current.Artifacts {
		m := matches[key(a)]
		if len(m) == 1 && counts[key(a)] == 1 && m[0].ProviderID != "" {
			current.Artifacts[i].ProviderID = m[0].ProviderID
			current.Artifacts[i].Observation = m[0].Observation
		}
	}
	current.Scope = cached.Scope
	current.CacheLoaded = true
	return reporting.Normalize(current), nil
}
func RunCLI(args []string, feature, owner string, catalog func(Config, string) ([]reporting.Artifact, error), stdout io.Writer) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", "", "nonsecret JSON object")
	env := fs.String("environment", "preview", "environment")
	dir := fs.String("output-dir", "deployment-reports/preview", "output directory relative to current directory")
	cache := fs.String("cache", "", "explicit app report JSON")
	scope := reporting.Scope{Repository: owner}
	fs.StringVar(&scope.Project, "project", "", "app project")
	fs.StringVar(&scope.Account, "account", "", "account identity")
	fs.StringVar(&scope.Backend, "backend", "", "backend identity")
	fs.StringVar(&scope.Region, "region", "", "region")
	fs.StringVar(&scope.Application, "application", "", "application")
	fs.StringVar(&scope.Stack, "stack", "", "stack")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *env == "" || *dir == "" {
		return fmt.Errorf("environment and output-dir required")
	}
	cfg := Config{}
	if *config != "" {
		b, e := os.ReadFile(*config)
		if e != nil {
			return fmt.Errorf("read configuration: %w", e)
		}
		if e = json.Unmarshal(b, &cfg); e != nil {
			return fmt.Errorf("config must be a nonsecret string map: %w", e)
		}
	}
	// Secrets belong only to offline validation fixtures, never user configuration.
	for k := range cfg {
		if strings.Contains(strings.ToLower(k), "secret") || strings.Contains(strings.ToLower(k), "key") || strings.Contains(strings.ToLower(k), "password") {
			return fmt.Errorf("secret configuration is not accepted")
		}
	}
	a, e := catalog(cfg, *env)
	if e != nil {
		return e
	}
	scope.Environment = *env
	r := reporting.Report{SchemaVersion: reporting.SchemaVersion, Scope: scope, Artifacts: a, Warnings: []string{"illustrative_offline_catalog"}}
	if *cache != "" {
		b, e := os.ReadFile(*cache)
		if e != nil {
			return fmt.Errorf("read cache: %w", e)
		}
		var cached reporting.Report
		if e = json.Unmarshal(b, &cached); e != nil {
			return fmt.Errorf("invalid cache")
		}
		r, e = Reconcile(r, cached, feature)
		if e != nil {
			return e
		}
	}
	if e = os.MkdirAll(*dir, 0700); e != nil {
		return e
	}
	for _, o := range []struct {
		name  string
		write func(io.Writer, reporting.Report) error
	}{{"report.md", reporting.WriteMarkdown}, {"report.json", reporting.WriteJSON}, {"report.html", reporting.WriteHTML}} {
		f, e := os.OpenFile(filepath.Join(*dir, o.name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		e = o.write(f, r)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	return reporting.WriteMarkdown(stdout, r)
}
