// Package ownership defines deployment ownership independently of a cloud SDK.
package ownership

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Metadata contains nonsecret deployment identifiers. Domain is an explicit DNS
// hostname, never a URL; shared resources use Environment="shared".
type Metadata struct{ Application, Environment, Domain, Feature, Deployment, ManagedBy, Owner string }

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var hostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

func (m Metadata) Validate() error {
	for _, v := range []struct{ name, value string }{{"application", m.Application}, {"environment", m.Environment}, {"feature", m.Feature}, {"deployment", m.Deployment}} {
		if !identifier.MatchString(v.value) {
			return fmt.Errorf("ownership %s must be a nonempty identifier of at most 64 characters", v.name)
		}
	}
	if len(m.Domain) > 253 || !hostname.MatchString(m.Domain) {
		return fmt.Errorf("ownership domain must be a lowercase DNS hostname")
	}
	for _, label := range strings.Split(m.Domain, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("ownership domain has an invalid DNS label")
		}
	}
	if m.Owner != "" && !identifier.MatchString(m.Owner) {
		return fmt.Errorf("ownership owner must be an identifier")
	}
	if m.ManagedBy != "" && !identifier.MatchString(m.ManagedBy) {
		return fmt.Errorf("ownership managed-by must be an identifier")
	}
	return nil
}
func (m Metadata) Tags(component string) (map[string]string, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if component == "" || len(component) > 256 || !regexp.MustCompile(`^[A-Za-z0-9_.:/@+=-]+$`).MatchString(component) {
		return nil, fmt.Errorf("ownership component must be a nonempty resource identifier of at most 256 characters")
	}
	managed := m.ManagedBy
	if managed == "" {
		managed = "infra-composer"
	}
	return map[string]string{"ic:application": m.Application, "ic:environment": m.Environment, "ic:domain": m.Domain, "ic:feature": m.Feature, "ic:component": component, "ic:deployment": m.Deployment, "ic:managed-by": managed}, nil
}

// Description renders domain, uppercase environment and the creating repository.
// Owner defaults to Feature for callers that do not supply a repository name.
// The purpose argument is retained for source compatibility.
func (m Metadata) Description(_ string, limit int) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	owner := m.Owner
	if owner == "" {
		owner = m.Feature
	}
	text := fmt.Sprintf("%s %s | %s", m.Domain, strings.ToUpper(m.Environment), owner)
	if limit > 0 && utf8.RuneCountInString(text) > limit {
		return "", fmt.Errorf("ownership description exceeds the service limit of %d characters", limit)
	}
	return text, nil
}

// MergeTags preserves unrelated labels and rejects changes to canonical ownership.
func (m Metadata) MergeTags(existing map[string]string, component string) (map[string]string, error) {
	canonical, err := m.Tags(component)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range canonical {
		if old, ok := out[k]; ok && old != v {
			return nil, fmt.Errorf("reserved ownership tag %s conflicts with this deployment", k)
		}
		out[k] = v
	}
	return out, nil
}
