package reportpreview

import (
	"bytes"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeRecorderNeverExportsInputsOrMockIdentity(t *testing.T) {
	a, e := Catalog("test", "f", "owner", func(ctx *pulumi.Context) error {
		var r pulumi.CustomResourceState
		return ctx.RegisterResource("aws:lambda/function:Function", "hello", pulumi.Map{"secret": pulumi.ToSecret("DO-NOT-REPORT")}, &r)
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(a) != 1 || a[0].ProviderID != "" || a[0].Observation.Status != reporting.Unknown {
		t.Fatal(a)
	}
	var b bytes.Buffer
	reporting.WriteJSON(&b, reporting.Report{Artifacts: a})
	if strings.Contains(b.String(), "DO-NOT-REPORT") || strings.Contains(b.String(), "offline-hello") {
		t.Fatal(b.String())
	}
}
func TestUnexpectedInvokeRejected(t *testing.T) {
	m := &recorder{}
	_, e := m.Call(pulumi.MockCallArgs{Token: "aws:index/getSecret:getSecret"})
	if e == nil {
		t.Fatal("accepted invoke")
	}
}
func TestArchivesPrivateAndRemoved(t *testing.T) {
	var path string
	e := Archives([]string{"test"}, func(dir string) error {
		path = filepath.Join(dir, "test.zip")
		s, e := os.Stat(path)
		if e != nil {
			return e
		}
		if s.Mode().Perm() != 0600 {
			t.Fatal(s.Mode())
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestCacheExactFeatureScopeAndAmbiguity(t *testing.T) {
	scope := reporting.Scope{Environment: "dev", Project: "app", Account: "123", Backend: "local"}
	a := reporting.Artifact{Key: "urn:pulumi:preview::offline::aws:x:y::name", Name: "name", Type: "aws:x:y", Feature: "f", Observation: reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}}
	cur := reporting.Report{SchemaVersion: 1, Scope: scope, Artifacts: []reporting.Artifact{a}}
	live := a
	live.Key = "urn:pulumi:dev::app::aws:x:y::name"
	live.ProviderID = "real"
	live.Observation = reporting.Observation{Status: reporting.Present, Source: "refresh", ObservedAt: time.Unix(12, 0)}
	cached := reporting.Report{SchemaVersion: 1, Scope: scope, Artifacts: []reporting.Artifact{live}}
	r, e := Reconcile(cur, cached, "f")
	if e != nil || r.Artifacts[0].ProviderID != "real" || r.Artifacts[0].Observation.ObservedAt != live.Observation.ObservedAt {
		t.Fatal(r, e)
	}
	cached.Artifacts = append(cached.Artifacts, live)
	r, e = Reconcile(cur, cached, "f")
	if e != nil || r.Artifacts[0].ProviderID != "" {
		t.Fatal(r, e)
	}
	cached.Scope.Environment = "prod"
	if _, e = Reconcile(cur, cached, "f"); e == nil {
		t.Fatal("accepted different environment")
	}
	cached.Scope = scope
	cached.Artifacts[0].Feature = "other"
	cached.Artifacts = cached.Artifacts[:1]
	if _, e = Reconcile(cur, cached, "f"); e == nil {
		t.Fatal("accepted different feature")
	}
}
func TestCLIErrors(t *testing.T) {
	for _, args := range [][]string{{"-bad"}, {"extra"}, {"-environment", ""}} {
		if e := RunCLI(args, "f", "owner", nil, &bytes.Buffer{}); e == nil {
			t.Fatal(args)
		}
	}
}
func TestCacheRejectsIdentityMismatch(t *testing.T) {
	base := reporting.Scope{Environment: "dev", Project: "app", Account: "123", Backend: "local"}
	for _, field := range []string{"project", "account", "backend"} {
		s := base
		switch field {
		case "project":
			s.Project = "other"
		case "account":
			s.Account = "456"
		case "backend":
			s.Backend = "other"
		}
		if _, e := Reconcile(reporting.Report{SchemaVersion: 1, Scope: s}, reporting.Report{SchemaVersion: 1, Scope: base}, "f"); e == nil {
			t.Fatal(field)
		}
	}
}
