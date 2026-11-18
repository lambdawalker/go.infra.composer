package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

type portableSettings struct {
	Version  int
	Project  string
	Settings Settings
}

// Export only explicitly portable, nonsecret values. No ownership, success,
// stack receipts, GitHub delivery receipts or vault bytes are transferable here.
func exportSettings(path, project string, s Settings) error {
	b, e := json.MarshalIndent(portableSettings{1, project, s}, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(path, b)
}
func importSettings(path, project, environment string) (Settings, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Settings{}, e
	}
	var p portableSettings
	if e = json.Unmarshal(b, &p); e != nil {
		return Settings{}, e
	}
	s := p.Settings
	if p.Version != 1 || p.Project != project || s.Version != 1 || s.Environment != environment || !regexp.MustCompile(`^\d{12}$`).MatchString(s.Account) || !regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`).MatchString(s.Region) || !regexp.MustCompile(`^s3://[a-z0-9.-]+/[^?\s]+$`).MatchString(s.Backend) {
		return Settings{}, fmt.Errorf("invalid or differently scoped portable settings")
	}
	return s, nil
}
