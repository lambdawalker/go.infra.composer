package pulumicli

import (
	"context"
	"errors"
	r "github.com/lambdawalker/go.infra.composer/reporting"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStateDependenciesOnlyExposeKnownManagedResourceKeys(t *testing.T) {
	input := `{"deployment":{"resources":[
		{"urn":"function","type":"aws:lambda/function:Function","parent":"component",
		 "dependencies":["role","key","function","provider","unknown-secret-value"],
		 "propertyDependencies":{"sensitive-property":["key","table"]}},
		{"urn":"role","type":"aws:iam/role:Role"},
		{"urn":"key","type":"aws:kms/key:Key"},
		{"urn":"table","type":"aws:dynamodb/table:Table"},
		{"urn":"component","type":"custom:app:Component","custom":false},
		{"urn":"provider","type":"pulumi:providers:aws"}]}}`
	got, err := ReadState(strings.NewReader(input), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || !slices.Equal(got[0].Dependencies, []string{"key", "role", "table"}) {
		t.Fatalf("incorrect dependency graph: %#v", got)
	}
	var body strings.Builder
	if err := r.WriteJSON(&body, r.Report{Artifacts: got}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.String(), "unknown-secret-value") || strings.Contains(body.String(), "sensitive-property") {
		t.Fatal("dependency property names or unresolved values escaped the state reader")
	}
}

func TestSavedPlanDependenciesOverrideOnlyKnownGoals(t *testing.T) {
	recorded := []r.Artifact{
		{Key: "lambda", Dependencies: []string{"old-role"}, ProviderID: "existing"},
		{Key: "old-role", Dependencies: []string{"key"}},
		{Key: "removed-edge", Dependencies: []string{"old-role"}},
		{Key: "new-role"},
		{Key: "key"},
	}
	input := `{"resourcePlans":{
		"lambda":{"goal":{"dependencies":["new-role"],"propertyDependencies":{"keyArn":["key","new-role","unmanaged"]}}},
		"old-role":{"steps":["delete"]},
		"removed-edge":{"goal":{}}}}`
	got, err := ApplyPlanDependencies(recorded, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[0].Dependencies, []string{"key", "new-role"}) ||
		!slices.Equal(got[1].Dependencies, []string{"key"}) ||
		len(got[2].Dependencies) != 0 || got[0].ProviderID != "existing" ||
		!slices.Equal(recorded[0].Dependencies, []string{"old-role"}) {
		t.Fatalf("incorrect planned links: %#v", got)
	}
}

func TestStateAllowlistAndParentInference(t *testing.T) {
	input := `{"deployment":{"resources":[{"urn":"bucket","id":"b","type":"aws:s3/bucket:Bucket","outputs":{"tags":{"ic:feature":"capture"},"password":"secret-marker"}},{"urn":"object","id":"b/k","type":"aws:s3/bucketObjectv2:BucketObjectv2","outputs":{"bucket":"b","key":"index.html","secret":{"4dabf18193072939515e22adb298388d":"secret-marker"}}}]}}`
	as, e := ReadState(strings.NewReader(input), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if len(as) != 2 || as[1].Parent != "bucket" || as[1].Name != "index.html" || as[0].Feature != "capture" {
		t.Fatalf("%#v", as)
	}
	var b strings.Builder
	r.WriteJSON(&b, r.Report{Artifacts: as})
	if strings.Contains(b.String(), "secret-marker") {
		t.Fatal("secret leak")
	}
}
func TestRefreshRequiresExactCompletedIdentity(t *testing.T) {
	at := time.Now()
	as := []r.Artifact{{Key: "parent", ProviderID: "p"}, {Key: "child", ProviderID: "c"}, {Key: "replacement", ProviderID: "new"}, {Key: "gone", ProviderID: "g"}}
	events := `{"resourceOutputsEvent":{"metadata":{"urn":"parent","op":"same","old":{"id":"p"},"new":{"id":"p"}}}}
{"resourceOutputsEvent":{"metadata":{"urn":"replacement","op":"same","old":{"id":"old"},"new":{"id":"old"}}}}
{"resourceOutputsEvent":{"metadata":{"urn":"gone","op":"delete","old":{"id":"g"}}}}
{"diagnosticEvent":{"message":"secret-marker"}}
{"resourceOutputsEvent":`
	got, e := ReadRefresh(strings.NewReader(events), as, at)
	if e == nil {
		t.Fatal("expected partial parse error")
	}
	if got[0].Observation.Status != r.Present || got[1].Observation.Status != r.Unknown || got[2].Observation.Status != r.Unknown || got[3].Observation.Status != r.Absent {
		t.Fatalf("%#v", got)
	}
}

type fakeRunner struct {
	t    *testing.T
	fail bool
}

func (f fakeRunner) Run(_ context.Context, _ string, args []string, _ io.Writer) error {
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "refresh --preview-only --non-interactive --json --event-log") {
		f.t.Fatal(joined)
	}
	path := args[len(args)-1]
	st, e := os.Stat(path)
	if e != nil || st.Mode().Perm() != 0600 {
		f.t.Fatal("private event file required")
	}
	if f.fail {
		return errors.New("secret-provider-error")
	}
	return os.WriteFile(path, []byte(`{"resourceOutputsEvent":{"metadata":{"urn":"a","op":"same","old":{"id":"id"},"new":{"id":"id"}}}}`), 0600)
}
func TestCollectorFailureUnknownAndPrivateEvents(t *testing.T) {
	as := []r.Artifact{{Key: "a", ProviderID: "id", Observation: r.Observation{Status: r.Present, ObservedAt: time.Now()}}}
	got, e := CollectRefresh(context.Background(), fakeRunner{t, true}, RefreshOptions{}, as)
	if e == nil || strings.Contains(e.Error(), "secret-provider-error") || got[0].Observation.Status != r.Unknown {
		t.Fatalf("%#v %v", got, e)
	}
	got, e = CollectRefresh(context.Background(), fakeRunner{t, false}, RefreshOptions{}, as)
	if e != nil || got[0].Observation.Status != r.Present {
		t.Fatalf("%#v %v", got, e)
	}
}
func TestPreviewPreservesPhysicalIdentity(t *testing.T) {
	as := []r.Artifact{{Key: "a", ProviderID: "old"}}
	got, e := ApplyPreview(as, strings.NewReader(`{"resourcePreEvent":{"metadata":{"urn":"a","type":"aws:x:X","op":"replace","old":{"id":"old"},"new":{"id":"mock-id"}}}}`))
	if e != nil || got[0].ProviderID != "old" || got[0].PlannedAction != "replace" {
		t.Fatalf("%#v %v", got, e)
	}
}

func TestRealRefreshMetadataAndMalformedAbsence(t *testing.T) {
	as := []r.Artifact{{Key: "a", ProviderID: "id"}, {Key: "b", ProviderID: "idb"}, {Key: "c", ProviderID: "idc"}, {Key: "d", ProviderID: "idd"}}
	events := `{"resourceOutputsEvent":{"metadata":{"urn":"a","op":"same","old":{"id":"id"},"new":{"id":"id"}}}}
{"resourceOutputsEvent":{"metadata":{"urn":"b","op":"delete","old":{"id":"idb"},"new":null}}}
{"resourceOutputsEvent":{"metadata":{"urn":"c","op":"refresh","old":{"id":"idc"}}}}
{"resourceOutputsEvent":{"metadata":{"urn":"d","op":"update","old":{"id":"idd"},"new":{"id":"idd"}}}}`
	got, e := ReadRefresh(strings.NewReader(events), as, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if got[0].Observation.Status != r.Present || got[1].Observation.Status != r.Absent || got[2].Observation.Status != r.Unknown {
		t.Fatalf("%#v", got)
	}
	if got[3].Observation.Status != r.Present || got[3].Observation.Reason != r.ExternalChange {
		t.Fatalf("expected completed update to identify drift, got %#v", got[3])
	}
}

func TestStateServiceParentsWithSharedProviderIDsAndStackParent(t *testing.T) {
	input := `{"deployment":{"resources":[{"urn":"stack","type":"pulumi:pulumi:Stack"},{"urn":"bucket","id":"b","type":"aws:s3/bucketV2:BucketV2","parent":"stack"},{"urn":"versioning","id":"b","type":"aws:s3/bucketVersioningV2:BucketVersioningV2","parent":"stack","outputs":{"bucket":"b"}},{"urn":"policy","id":"b","type":"aws:s3/bucketPolicy:BucketPolicy","parent":"stack","outputs":{"bucket":"b"}},{"urn":"object","id":"b/k","type":"aws:s3/bucketObjectv2:BucketObjectv2","parent":"stack","outputs":{"bucket":"b","key":"index.html"}}]}}`
	as, e := ReadState(strings.NewReader(input), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if len(as) != 4 {
		t.Fatalf("pseudo-resource included: %#v", as)
	}
	for _, a := range as {
		if a.Key != "bucket" && a.Parent != "bucket" {
			t.Fatalf("bad parent: %#v", a)
		}
	}
}

func TestFreshPreviewKeepsSafeHierarchyAndFeatureWithoutPhysicalIDs(t *testing.T) {
	input := `{"resourcePreEvent":{"metadata":{"urn":"object","type":"aws:s3/bucketObjectv2:BucketObjectv2","op":"create","new":{"custom":true,"id":"preview-object-id","parent":"bucket","inputs":{"tags":{"ic:feature":"capture","ic:owner":"org/feature"},"key":"index.html","password":"secret-marker"}}}}}
{"resourcePreEvent":{"metadata":{"urn":"bucket","type":"aws:s3/bucketV2:BucketV2","op":"create","new":{"custom":true,"id":"preview-bucket-id","inputs":{"tags":{"ic:feature":"capture","ic:owner":"org/feature"}}}}}}
{"resourcePreEvent":{"metadata":{"urn":"wrapped","type":"aws:s3/bucketPolicy:BucketPolicy","op":"create","new":{"custom":true,"inputs":{"tags":{"ic:feature":{"secret":"secret-marker"}}}}}}}`
	as, e := ApplyPreview(nil, strings.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	selected := r.SelectFeature(as, "capture")
	if len(selected) != 2 {
		t.Fatalf("lost feature: %#v", as)
	}
	nodes := r.Hierarchy(selected)
	if len(nodes) != 1 || nodes[0].Artifact.Key != "bucket" || len(nodes[0].Children) != 1 || nodes[0].Children[0].Artifact.Name != "index.html" {
		t.Fatalf("lost hierarchy: %#v", nodes)
	}
	for _, a := range selected {
		if a.ProviderID != "" || a.Observation.Status != r.Unknown || a.Owner != "org/feature" {
			t.Fatalf("unsafe preview: %#v", a)
		}
	}
	var b strings.Builder
	r.WriteJSON(&b, r.Report{Artifacts: as})
	if strings.Contains(b.String(), "secret-marker") {
		t.Fatal("leaked raw property")
	}
}

func TestPreviewFiltersPseudoResourcesAndResolvesAcrossComponent(t *testing.T) {
	input := `{"resourcePreEvent":{"metadata":{"urn":"stack","type":"pulumi:pulumi:Stack","op":"create","new":{"custom":false}}}}
{"resourcePreEvent":{"metadata":{"urn":"provider","type":"pulumi:providers:aws","op":"create","new":{"custom":true}}}}
{"resourcePreEvent":{"metadata":{"urn":"component","type":"example:app:Component","op":"create","new":{"custom":false}}}}
{"resourcePreEvent":{"metadata":{"urn":"bucket","type":"aws:s3/bucketV2:BucketV2","op":"create","new":{"custom":true,"id":"b","parent":"component","inputs":{"tags":{"ic:feature":"capture"}}}}}}
{"resourcePreEvent":{"metadata":{"urn":"object","type":"aws:s3/bucketObjectv2:BucketObjectv2","op":"create","new":{"custom":true,"parent":"component","inputs":{"bucket":"b","key":"index.html","tags":{"ic:feature":"capture"}}}}}}`
	as, e := ApplyPreview([]r.Artifact{{Key: "manual", Type: "example:app:Component"}}, strings.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	if len(as) != 4 {
		t.Fatalf("pseudo inventory: %#v", as)
	}
	for _, a := range as {
		if (a.Key == "object" || a.Key == "bucket") && a.Parent != "component" {
			t.Fatalf("lost component parent: %#v", a)
		}
		if a.ProviderID != "" {
			t.Fatal("adopted preview ID")
		}
	}
}

func TestStateTypedParentMatchesInternalARNWithoutPersistingProperties(t *testing.T) {
	input := `{"deployment":{"resources":[{"urn":"function","id":"reset-name","type":"aws:lambda/function:Function","outputs":{"arn":"arn:aws:lambda:us-east-2:123456789012:function:reset-name","secret":"do-not-persist"}},{"urn":"permission","id":"permission-id","type":"aws:lambda/permission:Permission","outputs":{"function":"arn:aws:lambda:us-east-2:123456789012:function:reset-name"}}]}}`
	as, e := ReadState(strings.NewReader(input), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if len(as) != 2 || as[1].Parent != "function" {
		t.Fatalf("ARN parent not matched: %#v", as)
	}
	var b strings.Builder
	r.WriteJSON(&b, r.Report{Artifacts: as})
	if strings.Contains(b.String(), "arn:aws:") || strings.Contains(b.String(), "do-not-persist") {
		t.Fatal("internal alias/property persisted")
	}
}

func TestStateARNAliasDeduplicatesURNAndRequiresExpectedType(t *testing.T) {
	input := `{"deployment":{"resources":[{"urn":"function","id":"same-arn","type":"aws:lambda/function:Function","outputs":{"arn":"same-arn"}},{"urn":"role","id":"role-id","type":"aws:iam/role:Role","outputs":{"arn":"same-arn"}},{"urn":"permission","id":"p","type":"aws:lambda/permission:Permission","outputs":{"function":"same-arn"}}]}}`
	as, e := ReadState(strings.NewReader(input), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if as[2].Parent != "function" {
		t.Fatalf("alias duplicate or wrong type: %#v", as)
	}
}

func TestComponentHierarchyFromStatePreviewAndPlan(t *testing.T) {
	const state = `{"deployment":{"resources":[
		{"urn":"key","type":"aws:kms/key:Key","custom":true,"parent":"group"},
		{"urn":"table","type":"aws:dynamodb/table:Table","custom":true,"parent":"group"},
		{"urn":"group","type":"example:app:Backend","custom":false,"parent":"stack"},
		{"urn":"stack","type":"pulumi:pulumi:Stack","custom":false}]}}`
	// Step state omits custom:false; children may arrive before their component.
	const events = `{"resourcePreEvent":{"metadata":{"urn":"key","op":"same","new":{"urn":"key","type":"aws:kms/key:Key","custom":true,"parent":"group"}}}}
{"resourcePreEvent":{"metadata":{"urn":"group","op":"same","new":{"urn":"group","type":"example:app:Backend","parent":"stack"}}}}
{"resourcePreEvent":{"metadata":{"urn":"table","op":"same","new":{"urn":"table","type":"aws:dynamodb/table:Table","custom":true,"parent":"group"}}}}`
	const plan = `{"resourcePlans":{
		"table":{"goal":{"custom":true,"type":"aws:dynamodb/table:Table","parent":"group"}},
		"group":{"goal":{"custom":false,"type":"example:app:Backend","parent":"stack"}},
		"key":{"goal":{"custom":true,"type":"aws:kms/key:Key","parent":"group"}},
		"stack":{"goal":{"custom":false,"type":"pulumi:pulumi:Stack"}}}}`
	legacy := []r.Artifact{{Key: "key", Type: "aws:kms/key:Key", ProviderID: "physical-key"}}
	check := func(as []r.Artifact, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		nodes := r.Hierarchy(as)
		if len(nodes) != 1 || nodes[0].Artifact.Key != "group" || !nodes[0].Artifact.Component || len(nodes[0].Children) != 2 {
			t.Fatalf("invalid hierarchy: %#v", as)
		}
		for _, child := range nodes[0].Children {
			if child.Artifact.Component || (child.Artifact.Key != "key" && child.Artifact.Key != "table") {
				t.Fatalf("unexpected group member: %#v", child.Artifact)
			}
		}
	}
	check(ReadState(strings.NewReader(state), time.Now()))
	check(ApplyPreview(legacy, strings.NewReader(events)))
	check(ApplyPlanDependencies(legacy, strings.NewReader(plan)))
}
