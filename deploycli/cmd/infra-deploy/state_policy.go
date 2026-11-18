package main

import (
	"encoding/json"
	"fmt"
)

func containsString(v any, want string) bool {
	if s, ok := v.(string); ok {
		return s == want
	}
	if list, ok := v.([]any); ok {
		for _, s := range list {
			if s == want {
				return true
			}
		}
	}
	return false
}
func requireStateTLS(raw []byte, bucket string) error {
	var response struct{ Policy string }
	if e := json.Unmarshal(raw, &response); e != nil {
		return e
	}
	var policy struct {
		Statement []struct {
			Effect                      string
			Principal, Action, Resource any
			Condition                   map[string]map[string]any
		}
	}
	if e := json.Unmarshal([]byte(response.Policy), &policy); e != nil {
		return e
	}
	for _, s := range policy.Statement {
		insecure := s.Condition["Bool"]["aws:SecureTransport"]
		principal := s.Principal == "*"
		if p, ok := s.Principal.(map[string]any); ok {
			principal = containsString(p["AWS"], "*")
		}
		if s.Effect == "Deny" && principal && (insecure == "false" || insecure == false) && (containsString(s.Action, "s3:*") || containsString(s.Action, "*")) && containsString(s.Resource, "arn:aws:s3:::"+bucket) && containsString(s.Resource, "arn:aws:s3:::"+bucket+"/*") {
			return nil
		}
	}
	return fmt.Errorf("state bucket must deny non-TLS requests for bucket and objects; have its owner reconcile the policy without replacing unrelated statements")
}
