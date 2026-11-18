package main

import "testing"

func TestDefaultAction(t *testing.T) {
	for _, tc := range []struct {
		action string
		ci     bool
		want   string
	}{
		{"", false, "deploy"}, {"", true, "preview"}, {"preview", false, "preview"}, {"preview", true, "preview"}, {"status", false, "status"}, {"build", false, "build"}, {"deploy", true, "deploy"},
	} {
		if got := defaultAction(tc.action, tc.ci); got != tc.want {
			t.Fatalf("%q CI=%t: %q, want %q", tc.action, tc.ci, got, tc.want)
		}
	}
}
