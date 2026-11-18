package main

import (
	"fmt"
	"strings"
)

// restoreStackConfig distinguishes an initialized stack from one with deployment history.
// A retry of a never-deployed stack must work just like its first invocation.
func restoreStackConfig(run func() ([]byte, error)) error {
	output, err := run()
	if err == nil {
		return nil
	}
	if strings.TrimSpace(string(output)) == "error: getting latest configuration: no previous deployment" {
		note("No deployment history yet; keeping local stack configuration")
		return nil
	}
	return fmt.Errorf("restore stack configuration: %w: %s", err, strings.TrimSpace(string(output)))
}
