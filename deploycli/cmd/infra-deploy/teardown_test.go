package main

import "testing"

func TestDiscoveryStackPreflight(t *testing.T) {
	index := discoveryStackSpec{PulumiProject: "email-verification-discovery", Stack: "shared"}
	for _, tt := range []struct {
		stacks string
		ok     bool
	}{
		{`[{"name":"shared"}]`, true},
		{`[{"name":"org/email-verification-discovery/shared"}]`, true},
		{`[{"name":"org/another-project/shared2"}]`, false},
		{`[]`, false},
		{`{"name":"shared"}`, false},
	} {
		err := requireDiscoveryStack([]byte(tt.stacks), index)
		if (err == nil) != tt.ok {
			t.Fatalf("stack listing %s returned %v", tt.stacks, err)
		}
	}
}
