// Package reportpreview executes native constructors with offline Pulumi mocks.
// Fixtures and mock identities are private implementation details, never evidence.
package reportpreview

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lambdawalker/go.infra.composer/reporting"
	"github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Config map[string]string

func (c Config) Get(k string) string     { return c[k] }
func (c Config) Require(k string) string { return c[k] }
func (c Config) RequireSecret(k string) pulumi.StringOutput {
	return pulumi.ToSecret(c[k]).(pulumi.StringOutput)
}
func (c Config) GetBool(k string) bool                   { return c[k] == "true" }
func (c Config) GetObject(k string, v interface{}) error { return json.Unmarshal([]byte(c[k]), v) }
func Defaults(c Config) Config {
	out := Config{"appOrigin": "https://app.example.test", "senderDomain": "example.test", "senderAddress": "verify@example.test"}
	for k, v := range c {
		out[k] = v
	}
	return out
}

// Archives supplies valid empty ZIPs only for the lifetime of an offline callback.
func Archives(names []string, run func(string) error) error {
	dir, e := os.MkdirTemp("", "native-preview-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	for _, n := range names {
		if filepath.Base(n) != n {
			return fmt.Errorf("invalid archive name")
		}
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		if e = w.Close(); e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(dir, n+".zip"), b.Bytes(), 0600); e != nil {
			return e
		}
	}
	return run(dir)
}

type recorder struct {
	mu                   sync.Mutex
	rows                 []map[string]any
	project, environment string
}

func (m *recorder) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	switch a.Token {
	case "aws:index/getRegion:getRegion":
		return resource.NewPropertyMapFromMap(map[string]any{"name": "us-east-1", "region": "us-east-1"}), nil
	case "aws:index/getCallerIdentity:getCallerIdentity":
		return resource.NewPropertyMapFromMap(map[string]any{"accountId": "000000000000", "arn": "arn:aws:iam::000000000000:root", "userId": "offline"}), nil
	}
	return nil, fmt.Errorf("unexpected offline invoke %s", a.Token)
}
func (m *recorder) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	out := a.Inputs.Copy()
	id := a.Name
	if n := a.Inputs["name"]; n.IsString() {
		id = n.StringValue()
	}
	for k, v := range map[string]string{"arn": "arn:aws:offline::000000000000:" + a.Name, "name": a.Name, "url": "https://example.test/" + a.Name, "invokeArn": "arn:offline:" + a.Name, "versionId": "offline", "apiEndpoint": "https://api.example.test", "executionArn": "arn:offline:api"} {
		if !out[resource.PropertyKey(k)].HasValue() {
			out[resource.PropertyKey(k)] = resource.NewStringProperty(v)
		}
	}
	if a.TypeToken == "aws:acm/certificate:Certificate" {
		out["domainValidationOptions"] = resource.NewPropertyValue([]any{map[string]any{"resourceRecordName": "_offline.example.test", "resourceRecordType": "CNAME", "resourceRecordValue": "offline.example.test"}})
	}
	if a.TypeToken == "aws:ses/domainDkim:DomainDkim" {
		out["dkimTokens"] = resource.NewPropertyValue([]any{"offline-a", "offline-b", "offline-c"})
	}
	if strings.HasPrefix(a.TypeToken, "aws:") && a.Custom {
		props := map[string]any{}
		for _, k := range []string{"bucket", "apiId", "role", "function", "functionName", "userPoolId", "rule", "secretId", "domainName", "key", "tags"} {
			v := a.Inputs[resource.PropertyKey(k)]
			if v.IsString() {
				props[k] = v.StringValue()
			} else if k == "tags" && v.IsObject() {
				tags := map[string]string{}
				for key, val := range v.ObjectValue() {
					if strings.HasPrefix(string(key), "ic:") && val.IsString() {
						tags[string(key)] = val.StringValue()
					}
				}
				props[k] = tags
			}
		}
		parent := ""
		if a.RegisterRPC != nil {
			parent = a.RegisterRPC.GetParent()
		}
		row := map[string]any{"urn": fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", m.environment, m.project, a.TypeToken, a.Name), "type": a.TypeToken, "id": id, "parent": parent, "outputs": props}
		m.mu.Lock()
		m.rows = append(m.rows, row)
		m.mu.Unlock()
	}
	return id, out, nil
}

// Catalog records actual constructors, retaining only safe metadata. It never runs a provider.
func Catalog(environment, feature, owner string, run pulumi.RunFunc) ([]reporting.Artifact, error) {
	m := &recorder{project: "offline-preview", environment: environment}
	if e := pulumi.RunErr(run, pulumi.WithMocks(m.project, environment, m)); e != nil {
		return nil, e
	}
	// Resolve exact mock ARN aliases only for registered service parents.
	aliases := map[string]string{}
	for _, row := range m.rows {
		id := row["id"].(string)
		urn := row["urn"].(string)
		logical := urn[strings.LastIndex(urn, "::")+2:]
		aliases["arn:aws:offline::000000000000:"+logical] = id
	}
	for _, row := range m.rows {
		props := row["outputs"].(map[string]any)
		for _, k := range []string{"bucket", "apiId", "role", "function", "functionName", "userPoolId", "rule", "secretId", "domainName"} {
			if v, ok := props[k].(string); ok {
				if id, exists := aliases[v]; exists {
					props[k] = id
				}
			}
		}
	}
	b, e := json.Marshal(map[string]any{"deployment": map[string]any{"resources": m.rows}})
	if e != nil {
		return nil, e
	}
	a, e := pulumicli.ReadState(bytes.NewReader(b), time.Time{})
	for i := range a {
		a[i].ProviderID = ""
		a[i].PlannedAction = "declared"
		a[i].Owner = owner
		a[i].Feature = feature
		a[i].Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}
	}
	return a, e
}
