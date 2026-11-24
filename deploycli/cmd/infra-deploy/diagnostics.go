package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const diagnosticTailLimit = 8 << 10

type diagnosticTail struct {
	data      []byte
	truncated bool
}

func (t *diagnosticTail) Write(p []byte) (int, error) {
	if len(p) >= diagnosticTailLimit {
		t.truncated = t.truncated || len(p) > diagnosticTailLimit || len(t.data) != 0
		t.data = append(t.data[:0], p[len(p)-diagnosticTailLimit:]...)
	} else {
		if drop := len(t.data) + len(p) - diagnosticTailLimit; drop > 0 {
			t.truncated = true
			copy(t.data, t.data[drop:])
			t.data = t.data[:len(t.data)-drop]
		}
		t.data = append(t.data, p...)
	}
	return len(p), nil
}

func (t diagnosticTail) String() string {
	if !t.truncated {
		return string(t.data)
	}
	// The first line may be partial, including a credential cut by the buffer.
	_, rest, _ := bytes.Cut(t.data, []byte("\n"))
	return "[Earlier output omitted]\n" + string(rest)
}

// Only capture output from known commands. Never capture authentication output,
// raw configuration, stack exports, or the command's argument values.
func diagnosticCommand(args []string) (label string, includeStdout bool) {
	if len(args) < 2 {
		return "", false
	}
	switch args[0] {
	case "go":
		if args[1] == "build" {
			return "go build", false
		}
	case "pulumi":
		switch args[1] {
		case "preview", "up", "destroy":
			return "pulumi " + args[1], true
		case "stack":
			return "pulumi " + args[1], false
		}
	case "aws":
		if args[1] == "s3api" || args[1] == "sts" {
			return "aws " + args[1], false
		}
	}
	return "", false
}

func captureDiagnostics(ctx context.Context, c *exec.Cmd) func(error) {
	label, includeStdout := diagnosticCommand(c.Args)
	browser := wizard(ctx).browser
	if browser == nil || label == "" {
		return func(error) {}
	}

	// A child may inherit an output pipe after the command has exited.
	c.WaitDelay = 2 * time.Second
	var stderr, stdout diagnosticTail
	stderrWriter := c.Stderr
	if stderrWriter == nil {
		stderrWriter = os.Stderr
	}
	c.Stderr = io.MultiWriter(stderrWriter, &stderr)
	if includeStdout && c.Stdout != nil {
		c.Stdout = io.MultiWriter(c.Stdout, &stdout)
	}
	return func(err error) {
		if err != nil && ctx.Err() == nil {
			browser.CommandFailure(label, err, strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
		}
	}
}
