package pulumicli

import (
	"context"
	"errors"
	r "github.com/lambdawalker/go.infra.composer/reporting"
	"io"
	"os"
	"time"
)

type Runner interface {
	Run(context.Context, string, []string, io.Writer) error
}
type RefreshOptions struct {
	Dir        string
	Stack      string
	ObservedAt time.Time
}

var ErrRefresh = errors.New("Pulumi read-only refresh incomplete")

// CollectRefresh requests a read-only preview and reads a private event log, removed on return.
// Runner must execute Pulumi and return a failure for nonzero exit. No diagnostics are retained.
func CollectRefresh(ctx context.Context, runner Runner, opts RefreshOptions, as []r.Artifact) ([]r.Artifact, error) {
	at := opts.ObservedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	unknown, _ := ReadRefresh(nilReader{}, as, at)
	for i := range unknown {
		unknown[i].Observation.Reason = r.CheckFailed
	}
	f, e := os.CreateTemp("", "composer-refresh-*.json")
	if e != nil {
		return unknown, ErrRefresh
	}
	path := f.Name()
	defer os.Remove(path)
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return unknown, ErrRefresh
	}
	f.Close()
	args := []string{"refresh", "--preview-only", "--non-interactive", "--json"}
	if opts.Stack != "" {
		args = append(args, "--stack", opts.Stack)
	}
	args = append(args, "--event-log", path)
	if runner == nil {
		return unknown, ErrRefresh
	}
	runErr := runner.Run(ctx, opts.Dir, args, io.Discard)
	f, e = os.Open(path)
	if e != nil {
		return unknown, ErrRefresh
	}
	defer f.Close()
	out, parseErr := ReadRefresh(f, as, at)
	if runErr != nil || parseErr != nil {
		for i := range out {
			if out[i].Observation.Status == r.Unknown {
				out[i].Observation.Reason = r.CheckFailed
			}
		}
		return out, ErrRefresh
	}
	return out, nil
}

type nilReader struct{}

func (nilReader) Read([]byte) (int, error) { return 0, io.EOF }
