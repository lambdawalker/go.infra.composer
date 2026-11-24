package main

import (
	"strings"
	"testing"
)

func TestDiagnosticTailKeepsOnlyRecentCompleteLines(t *testing.T) {
	var tail diagnosticTail
	first := "credential-" + strings.Repeat("a", diagnosticTailLimit)
	for _, chunk := range []string{first, "\nrecent ", "error\n"} {
		if n, err := tail.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("write returned %d, %v", n, err)
		}
	}
	if got := tail.String(); got != "[Earlier output omitted]\nrecent error\n" {
		t.Fatalf("unexpected tail: %q", got)
	}
}
