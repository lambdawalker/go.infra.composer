// Command infra-nuke performs scoped, report-backed infrastructure teardown.
// Credentials are supplied to installed provider CLIs by their normal login flow.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/lambdawalker/go.infra.composer/teardown"
	"github.com/lambdawalker/go.infra.composer/teardown/pulumicli"
)

type driverFactory func(pulumicli.Config) (teardown.Driver, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, nil))
}

func run(args []string, input io.Reader, out, stderr io.Writer, factory driverFactory) int {
	return runContext(context.Background(), args, input, out, stderr, factory)
}

func runContext(ctx context.Context, args []string, input io.Reader, out, stderr io.Writer, factory driverFactory) int {
	f := flag.NewFlagSet("infra-nuke", flag.ContinueOnError)
	f.SetOutput(stderr)
	configPath := f.String("config", "", "JSON configuration of the application/account and all owned Pulumi projects/backends")
	reportPath := f.String("report", "", "Private persistent report path (default beside config in .infra-composer)")
	nuke := f.Bool("nuke", false, "Destroy all configured environments, then shared admin/index; requires typed confirmation")
	check := f.Bool("check", false, "Verify every artifact captured in the saved report; no deletion")
	fast := f.Bool("fast-check", false, "Verify only artifacts marked REMAINS by the latest check; no deletion")
	asJSON := f.Bool("json", false, "Emit the report as JSON")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	modes := 0
	for _, enabled := range []bool{*nuke, *check, *fast} {
		if enabled {
			modes++
		}
	}
	if modes != 1 || f.NArg() != 0 || *configPath == "" {
		fmt.Fprintln(stderr, "Specify -config and exactly one of -nuke, -check, or -fast-check; no positional arguments.")
		return 2
	}
	path, err := filepath.Abs(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, "Invalid configuration path.")
		return 2
	}
	cfg, err := loadConfig(path)
	if err != nil {
		fmt.Fprintln(stderr, "Cannot read a valid nuke configuration.")
		return 2
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`).MatchString(cfg.Scope.Application) || !regexp.MustCompile(`^[0-9]{12}$`).MatchString(cfg.Scope.Account) {
		fmt.Fprintln(stderr, "Configuration requires an application identifier and twelve-digit AWS account.")
		return 2
	}
	for i := range cfg.Projects {
		if !filepath.IsAbs(cfg.Projects[i].Directory) {
			cfg.Projects[i].Directory = filepath.Join(filepath.Dir(path), cfg.Projects[i].Directory)
		}
	}
	if *reportPath == "" {
		*reportPath = filepath.Join(filepath.Dir(path), ".infra-composer", "nuke."+cfg.Scope.Application+"."+cfg.Scope.Account+".json")
	}
	if err := os.MkdirAll(filepath.Dir(*reportPath), 0700); err != nil {
		fmt.Fprintln(stderr, "Cannot prepare report directory.")
		return 1
	}
	lockPath := *reportPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(stderr, "Report is locked or inaccessible; inspect its .lock file before retrying.")
		return 1
	}
	fmt.Fprintf(lock, "pid %d\n", os.Getpid())
	lock.Close()
	defer os.Remove(lockPath)
	if *nuke {
		challenge := "nuke " + cfg.Scope.Application + " " + cfg.Scope.Account
		fmt.Fprintf(stderr, "Destroy ALL environments in %d configured project(s), including shared admin/index. Application data is permanent.\n", len(cfg.Projects))
		for _, p := range cfg.Projects {
			fmt.Fprintf(stderr, "  project %s • %s • %s • shared=%t\n", p.Name, p.Backend, p.Region, p.Shared)
		}
		for _, a := range cfg.Recovery {
			fmt.Fprintf(stderr, "  recovery LAST %s • %s • %s • exclusive=%s\n", a.Kind, a.ID, a.Region, a.Data["exclusive"])
		}
		fmt.Fprintln(stderr, "Recovery artifacts are processed last, only after verified cleanup. Shared infrastructure outside this scope is retained.")
		fmt.Fprintf(stderr, "Type %q to authorize: ", challenge)
		line, readErr := bufio.NewReader(input).ReadString('\n')
		if (readErr != nil && !errors.Is(readErr, io.EOF)) || strings.TrimSpace(line) != challenge {
			fmt.Fprintln(stderr, "Confirmation did not match; no deletion was started.")
			return 1
		}
	}
	if factory == nil {
		factory = func(c pulumicli.Config) (teardown.Driver, error) { return pulumicli.New(c, nil) }
	}
	driver, err := factory(cfg)
	if err != nil {
		fmt.Fprintln(stderr, "Cannot initialize teardown driver:", err)
		return 1
	}
	e := teardown.Engine{Scope: cfg.Scope, Driver: driver, Store: teardown.FileStore{Path: *reportPath}, Recovery: cfg.Recovery}
	var report *teardown.Report
	if *nuke {
		report, err = e.Nuke(ctx)
	} else {
		report, err = e.Check(ctx, *fast)
	}
	if report != nil {
		if *asJSON {
			if encodeErr := json.NewEncoder(out).Encode(report); encodeErr != nil {
				fmt.Fprintln(stderr, "Cannot write report output.")
				return 1
			}
		} else {
			printReport(out, report, !*nuke)
		}
		fmt.Fprintln(stderr, "Report:", *reportPath)
	}
	if err != nil {
		fmt.Fprintln(stderr, "Teardown:", err)
		return 1
	}
	return 0
}

func loadConfig(path string) (pulumicli.Config, error) {
	var cfg pulumicli.Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 2<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return cfg, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return cfg, errors.New("unexpected trailing configuration")
	}
	return cfg, nil
}

func printReport(out io.Writer, r *teardown.Report, checking bool) {
	results := r.Results
	if checking {
		results = r.Checks
		fmt.Fprintf(out, "Check scope: %s; individual timestamps identify refreshed evidence.\n", r.CheckScope)
	}
	environments := map[string]string{}
	for _, t := range r.Targets {
		environments[t.ID] = t.Environment
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tARTIFACT TYPE\tREGION\tENVIRONMENT\tARTIFACT\tDETAIL")
	for _, result := range results {
		a := result.Artifact
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", result.Status, a.Kind, a.Region, environments[a.TargetID], a.ID, result.Detail)
	}
	w.Flush()
}
