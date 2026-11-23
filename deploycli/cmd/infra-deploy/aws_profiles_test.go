package main

import (
	"testing"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

func TestSelectRefreshedAWSProfile(t *testing.T) {
	old := []wizardweb.Option{{Value: "default"}, {Value: "existing"}}
	for _, test := range []struct {
		name    string
		current []wizardweb.Option
		want    string
	}{
		{"no profiles", nil, ""},
		{"same profiles", old, "existing"},
		{"new profile", []wizardweb.Option{{Value: "default"}, {Value: "existing"}, {Value: "added"}}, "added"},
		{"selection removed", []wizardweb.Option{{Value: "default"}}, "default"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := selectRefreshedProfile("existing", old, test.current); got != test.want {
				t.Fatalf("selected %q, want %q", got, test.want)
			}
		})
	}
}
