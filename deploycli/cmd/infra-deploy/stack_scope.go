package main

import (
	"encoding/json"
	"fmt"
)

// Inspect authoritative stack outputs before writing deployment identity config.
// A different project's shared backend must not become an accidental adoption.
func verifyStackIdentity(raw []byte, project, environment string, adoptLegacy bool) error {
	var state struct {
		Deployment *struct {
			Resources []struct {
				Type    string
				Outputs map[string]json.RawMessage
			}
		}
	}
	if e := json.Unmarshal(raw, &state); e != nil {
		return e
	}
	if state.Deployment == nil {
		return fmt.Errorf("invalid remote state: missing deployment")
	}
	managed := false
	for _, r := range state.Deployment.Resources {
		if r.Type != "pulumi:pulumi:Stack" && !isProviderType(r.Type) {
			managed = true
		}
		if r.Type != "pulumi:pulumi:Stack" {
			continue
		}
		if value, ok := r.Outputs["deploymentIdentity"]; ok {
			var identity string
			if json.Unmarshal(value, &identity) != nil || identity != project+"-"+environment {
				return fmt.Errorf("remote stack identity differs from project/environment; recover or select the original scope")
			}
			return nil
		}
	}
	if managed && !adoptLegacy {
		return fmt.Errorf("existing stack lacks deployment identity metadata; verify original ownership and rerun interactively with --adopt-legacy-stack")
	}
	return nil
}
func isProviderType(s string) bool { return len(s) >= 17 && s[:17] == "pulumi:providers:" }
