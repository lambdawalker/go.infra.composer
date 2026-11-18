package main

import (
	"errors"
	"testing"
)

func TestRestoreStackConfig(t *testing.T) {
	for _, tc := range []struct {
		name, output      string
		failed, wantError bool
	}{
		{"existing deployment", "", false, false},
		{"new stack", "error: getting latest configuration: no previous deployment\n", true, false},
		{"retry undeployed stack", "error: getting latest configuration: no previous deployment\r\n", true, false},
		{"access denied", "error: access denied", true, true},
		{"connection failed", "error: connection failed", true, true},
		{"unrecognized failure", "", true, true},
		{"mixed failure", "error: getting latest configuration: no previous deployment\nerror: access denied", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := restoreStackConfig(func() ([]byte, error) {
				if tc.failed {
					return []byte(tc.output), errors.New("exit status 1")
				}
				return []byte(tc.output), nil
			})
			if (e != nil) != tc.wantError {
				t.Fatal(e)
			}
		})
	}
}
