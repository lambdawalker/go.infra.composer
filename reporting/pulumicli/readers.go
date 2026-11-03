// Package pulumicli consumes allowlisted Pulumi identities and completion events.
package pulumicli

import (
	"bufio"
	"encoding/json"
	"errors"
	r "github.com/lambdawalker/go.infra.composer/reporting"
	"io"
	"strings"
	"time"
)

var ErrInvalidInput = errors.New("invalid Pulumi report input")

type resource struct {
	Custom  *bool                      `json:"custom"`
	URN     string                     `json:"urn"`
	ID      string                     `json:"id"`
	Type    string                     `json:"type"`
	Parent  string                     `json:"parent"`
	Outputs map[string]json.RawMessage `json:"outputs"`
	Inputs  map[string]json.RawMessage `json:"inputs"`
}

func plain(m map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(m[key], &s)
	return s
}
func name(urn string) string     { p := strings.Split(urn, "::"); return p[len(p)-1] }
func provider(typ string) string { return strings.Split(typ, ":")[0] }

func managed(s resource) bool {
	return s.URN != "" && s.Type != "" && !strings.HasPrefix(s.Type, "pulumi:") && (s.Custom == nil || *s.Custom)
}
func parentType(field, typ string) bool {
	parts := strings.Split(strings.ToLower(typ), ":")
	kind := parts[len(parts)-1]
	switch field {
	case "bucket":
		return kind == "bucket" || kind == "bucketv2"
	case "apiId":
		return kind == "api" || kind == "restapi"
	case "role", "roleArn":
		return kind == "role"
	case "function", "functionArn":
		return kind == "function"
	case "userPoolId", "userPoolArn":
		return kind == "userpool"
	case "rule", "ruleArn":
		return kind == "eventrule" || kind == "rule"
	case "secretId", "secretArn":
		return kind == "secret"
	case "domainName":
		return kind == "domainname" || kind == "domain"
	}
	return false
}

// ReadState reads `pulumi stack export` JSON. State is recorded, never live evidence.
// Only identity, parent links, canonical ownership tags and explicit relationship fields survive.
func ReadState(rd io.Reader, at time.Time) ([]r.Artifact, error) {
	var root struct {
		Deployment struct {
			Resources []resource `json:"resources"`
		} `json:"deployment"`
	}
	d := json.NewDecoder(rd)
	if e := d.Decode(&root); e != nil {
		return nil, ErrInvalidInput
	}
	if root.Deployment.Resources == nil {
		return nil, ErrInvalidInput
	}
	return safeArtifacts(root.Deployment.Resources, r.Observation{Status: r.Recorded, Source: "pulumi-state", ObservedAt: at}), nil
}

// safeArtifacts is shared by state and preview extraction. Provider IDs are only
// internal relationship keys during preview; ApplyPreview clears them before return.
func safeArtifacts(resources []resource, observation r.Observation) []r.Artifact {
	var as []r.Artifact
	ids := map[string][]resource{}
	byURN := map[string]resource{}
	for _, s := range resources {
		if !managed(s) {
			continue
		}
		byURN[s.URN] = s
		// ARN is an internal, allowlisted alias only; never copied into the report.
		aliases := map[string]bool{}
		for _, alias := range []string{s.ID, plain(s.Outputs, "arn")} {
			if alias != "" && !aliases[alias] {
				ids[alias] = append(ids[alias], s)
				aliases[alias] = true
			}
		}
	}
	for _, s := range resources {
		if !managed(s) {
			continue
		}
		a := r.Artifact{Key: s.URN, Name: name(s.URN), Type: s.Type, Provider: provider(s.Type), ProviderID: s.ID, Parent: s.Parent, Observation: observation}
		var tags map[string]json.RawMessage
		_ = json.Unmarshal(s.Outputs["tags"], &tags)
		a.Owner = plain(tags, "ic:owner")
		a.Feature = plain(tags, "ic:feature")
		var inputTags map[string]json.RawMessage
		_ = json.Unmarshal(s.Inputs["tags"], &inputTags)
		if a.Owner == "" {
			a.Owner = plain(inputTags, "ic:owner")
		}
		if a.Feature == "" {
			a.Feature = plain(inputTags, "ic:feature")
		}
		if _, ok := byURN[a.Parent]; !ok {
			a.Parent = ""
		}
		if a.Parent == "" {
			for _, field := range []string{"bucket", "apiId", "role", "roleArn", "function", "functionArn", "userPoolId", "userPoolArn", "rule", "ruleArn", "secretId", "secretArn", "domainName"} {
				id := plain(s.Outputs, field)
				if id == "" {
					id = plain(s.Inputs, field)
				}
				var candidates []string
				seen := map[string]bool{}
				for _, p := range ids[id] {
					if p.URN != a.Key && parentType(field, p.Type) && !seen[p.URN] {
						candidates = append(candidates, p.URN)
						seen[p.URN] = true
					}
				}
				if id != "" && len(candidates) == 1 {
					a.Parent = candidates[0]
					break
				}
			}
		}
		if strings.Contains(strings.ToLower(s.Type), "bucketobject") {
			key := plain(s.Outputs, "key")
			if key == "" {
				key = plain(s.Inputs, "key")
			}
			if key != "" {
				a.Name = key
			}
		}
		a.Priority = Priority(s.Type)
		as = append(as, a)
	}
	return as
}

// Priority implements broad display tiers. Adapters may override with domain priorities.
func Priority(typ string) int {
	t := strings.ToLower(typ)
	for _, s := range []string{"domain", "endpoint", "distribution"} {
		if strings.Contains(t, s) {
			return 10
		}
	}
	for _, s := range []string{"bucketobject", "policyattachment", "permission", "route", "integration"} {
		if strings.Contains(t, s) {
			return 60
		}
	}
	for _, s := range []string{"bucket", "table", "secret", "role", "policy", "userpool"} {
		if strings.Contains(t, s) {
			return 20
		}
	}
	for _, s := range []string{"function", "api", "queue", "rule", "schedule"} {
		if strings.Contains(t, s) {
			return 30
		}
	}
	return 40
}

type metadata struct {
	URN  string `json:"urn"`
	Type string `json:"type"`
	Op   string `json:"op"`

	Old *resource `json:"old"`
	New *resource `json:"new"`
}
type event struct {
	Pre *struct {
		Metadata metadata `json:"metadata"`
	} `json:"resourcePreEvent"`
	Outputs *struct {
		Metadata metadata `json:"metadata"`
	} `json:"resourceOutputsEvent"`
}

func events(rd io.Reader, visit func(event)) error {
	s := bufio.NewScanner(rd)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for s.Scan() {
		if len(strings.TrimSpace(s.Text())) == 0 {
			continue
		}
		var e event
		if json.Unmarshal(s.Bytes(), &e) != nil {
			return ErrInvalidInput
		}
		visit(e)
	}
	if s.Err() != nil {
		return ErrInvalidInput
	}
	return nil
}
func allowedAction(s string) string {
	switch s {
	case "same", "create", "update", "delete", "replace", "create-replacement", "delete-replaced", "read", "refresh", "import", "import-replacement":
		return s
	}
	return ""
}

// ApplyPreview adds intended actions; preview IDs never replace recorded physical IDs.
func ApplyPreview(as []r.Artifact, rd io.Reader) ([]r.Artifact, error) {
	out := append([]r.Artifact(nil), as...)
	index := map[string]int{}
	resourceIndex := map[string]int{}
	var resources []resource
	for i, a := range out {
		index[a.Key] = i
		resourceIndex[a.Key] = len(resources)
		resources = append(resources, resource{URN: a.Key, Type: a.Type, ID: a.ProviderID, Parent: a.Parent})
	}
	actions := map[string]string{}
	parseErr := events(rd, func(e event) {
		if e.Pre == nil {
			return
		}
		m := e.Pre.Metadata
		action := allowedAction(m.Op)
		if m.URN == "" || action == "" {
			return
		}
		s := resource{}
		if m.New != nil {
			s = *m.New
		} else if m.Old != nil {
			s = *m.Old
		}
		s.URN = m.URN
		if m.Type != "" {
			s.Type = m.Type
		}
		if s.Type == "" {
			if i, ok := index[m.URN]; ok {
				s.Type = out[i].Type
			}
		}
		if !managed(s) {
			return
		}
		if i, ok := resourceIndex[s.URN]; ok {
			resources[i] = s
		} else {
			resourceIndex[s.URN] = len(resources)
			resources = append(resources, s)
		}
		actions[m.URN] = action
	})
	// Resolve after the entire (possibly partial) stream so children may precede parents.
	for _, a := range safeArtifacts(resources, r.Observation{Status: r.Unknown, Reason: r.Unchecked}) {
		action, changed := actions[a.Key]
		if !changed {
			continue
		}
		if i, ok := index[a.Key]; ok {
			out[i].PlannedAction = action
			continue
		}
		a.ProviderID = ""
		a.PlannedAction = action
		index[a.Key] = len(out)
		out = append(out, a)
	}
	return out, parseErr
}

// ReadRefresh starts every artifact unknown. Only exact physical identity resourceOutputsEvent
// completions with op same/update/delete supply current evidence. Call only with a
// read-only refresh event stream; normal preview outputs do not establish presence.
func ReadRefresh(rd io.Reader, as []r.Artifact, at time.Time) ([]r.Artifact, error) {
	out := append([]r.Artifact(nil), as...)
	index := map[string]int{}
	for i, a := range out {
		index[a.Key] = i
		out[i].Observation = r.Observation{Status: r.Unknown, Source: "pulumi-refresh", ObservedAt: at, Reason: r.Unchecked}
	}
	e := events(rd, func(e event) {
		if e.Outputs == nil {
			return
		}
		m := e.Outputs.Metadata
		i, ok := index[m.URN]
		if !ok || out[i].ProviderID == "" {
			return
		}
		if m.Type != "" && out[i].Type != "" && m.Type != out[i].Type {
			return
		}
		status := r.Unknown
		reason := r.InvalidEvidence
		switch m.Op {
		case "same", "update":
			if m.New != nil && m.New.ID == out[i].ProviderID {
				status = r.Present
				reason = ""
			}
		case "delete":
			if m.Old != nil && m.Old.ID == out[i].ProviderID && m.New == nil {
				status = r.Absent
				reason = r.NotFound
			}
		}
		out[i].Observation = r.Observation{Status: status, Source: "pulumi-refresh", ObservedAt: at, Reason: reason}
	})
	return out, e
}
