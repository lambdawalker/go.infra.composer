package pulumicli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	r "github.com/lambdawalker/go.infra.composer/reporting"
)

// Moving a component changes both its URN and its descendants' URNs. Preview
// must reconcile Pulumi's explicit old/new identities with recorded inventory.
func TestPreviewReparentsRecordedInventoryWithoutDuplicates(t *testing.T) {
	const scope = "urn:pulumi:dev::app::"
	const group = scope + "example:Feature::email-verification"
	const oldBackend = scope + "example:Backend::verification.backend"
	const backend = scope + "example:Feature$example:Backend::verification.backend"
	const oldTable = scope + "example:Backend$aws:dynamodb/table:Table::transactions"
	const table = scope + "example:Feature$example:Backend$aws:dynamodb/table:Table::transactions"
	at := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	observation := r.Observation{Status: r.Recorded, Source: "pulumi-state", ObservedAt: at}
	recorded := []r.Artifact{
		{Key: oldBackend, Name: "verification.backend", Type: "example:Backend", Component: true, Observation: observation},
		{Key: oldTable, Name: "transactions", Type: "aws:dynamodb/table:Table", Provider: "aws", ProviderID: "actual-table", Parent: oldBackend, Observation: observation},
		{Key: "untouched", Type: "aws:lambda/function:Function", Parent: oldBackend, Dependencies: []string{oldTable}},
	}
	makeEvent := func(urn, typ, old, parent string, custom bool) string {
		m := map[string]any{"urn": urn, "type": typ, "op": "same", "new": map[string]any{"urn": urn, "type": typ, "parent": parent, "custom": custom, "id": "preview-id"}}
		if old != "" {
			m["old"] = map[string]any{"urn": old, "type": typ, "custom": custom, "id": "actual-table"}
		}
		b, err := json.Marshal(map[string]any{"resourcePreEvent": map[string]any{"metadata": m}})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	events := []string{
		makeEvent(table, "aws:dynamodb/table:Table", oldTable, backend, true),
		makeEvent(backend, "example:Backend", oldBackend, group, false),
		makeEvent(group, "example:Feature", "", "", false),
	}
	for _, reverse := range []bool{false, true} {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("reverse=%t/cached=%t", reverse, cached), func(t *testing.T) {
				input := slices.Clone(recorded)
				if cached {
					// A cached previous preview may already contain the planned key,
					// but only the original state carries a recorded physical identity.
					input = append(input, r.Artifact{Key: table, Name: "transactions", Type: "aws:dynamodb/table:Table", Parent: backend})
				}
				before := slices.Clone(input)
				lines := slices.Clone(events)
				if reverse {
					slices.Reverse(lines)
				}
				stream := strings.Join(lines, "\n")
				got, err := ApplyPreview(input, strings.NewReader(stream))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(input, before) {
					t.Fatal("preview mutated recorded inventory")
				}
				for pass := 0; pass < 2; pass++ {
					if len(got) != 4 {
						t.Fatalf("expected one feature, backend, table and untouched resource; got %d artifacts: %#v", len(got), got)
					}
					byKey := map[string]r.Artifact{}
					for _, a := range got {
						byKey[a.Key] = a
					}
					if byKey[backend].Parent != group || !byKey[backend].Component || byKey[table].Parent != backend {
						t.Fatalf("lost migrated hierarchy: %#v", got)
					}
					if byKey[table].ProviderID != "actual-table" || byKey[table].Observation != observation || byKey[table].PlannedAction != "same" {
						t.Fatalf("lost recorded evidence or adopted preview identity: %#v", byKey[table])
					}
					if byKey["untouched"].Parent != backend || !slices.Equal(byKey["untouched"].Dependencies, []string{table}) {
						t.Fatalf("retained stale relationship keys: %#v", byKey["untouched"])
					}
					if len(r.Hierarchy(got)) != 1 {
						t.Fatalf("old component still appears as a root: %#v", got)
					}
					got, err = ApplyPreview(got, strings.NewReader(stream))
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestPreviewDoesNotMergeNamesWithoutExplicitIdentityMapping(t *testing.T) {
	const oldKey = "urn:pulumi:dev::app::example:OldParent$example:Backend::backend"
	const newKey = "urn:pulumi:dev::app::example:NewParent$example:Backend::backend"
	as := []r.Artifact{{Key: oldKey, Name: "backend", Type: "example:Backend", Component: true}}
	stream := fmt.Sprintf(`{"resourcePreEvent":{"metadata":{"urn":%q,"type":"example:Backend","op":"delete","old":{"urn":%q,"type":"example:Backend"}}}}
{"resourcePreEvent":{"metadata":{"urn":%q,"type":"example:Backend","op":"create","new":{"urn":%q,"type":"example:Backend"}}}}`, oldKey, oldKey, newKey, newKey)
	got, err := ApplyPreview(as, strings.NewReader(stream))
	if err != nil || len(got) != 2 || got[0].Key != oldKey || got[1].Key != newKey || got[0].Name != got[1].Name || got[0].PlannedAction != "delete" || got[1].PlannedAction != "create" {
		t.Fatalf("unrelated inventory was discarded: %#v %v", got, err)
	}
}

func TestPreviewMovingToRootClearsRecordedParent(t *testing.T) {
	as := []r.Artifact{
		{Key: "parent", Type: "example:Group", Component: true},
		{Key: "old", Name: "old", Type: "example:Backend", Component: true, Parent: "parent"},
	}
	stream := `{"resourcePreEvent":{"metadata":{"urn":"new","type":"example:Backend","op":"same","old":{"urn":"old","type":"example:Backend","parent":"parent"},"new":{"urn":"new","type":"example:Backend","parent":""}}}}`
	got, err := ApplyPreview(as, strings.NewReader(stream))
	if err != nil || len(got) != 2 || got[1].Key != "new" || got[1].Name != "new" || got[1].Parent != "" {
		t.Fatalf("move to root kept old identity/parent: %#v %v", got, err)
	}
}

func TestPreviewKeepsRecordedTargetIdentityOverStaleAlias(t *testing.T) {
	as := []r.Artifact{
		{Key: "old", Type: "aws:s3/bucket:Bucket", ProviderID: "stale"},
		{Key: "new", Type: "aws:s3/bucket:Bucket", ProviderID: "current"},
	}
	stream := `{"resourcePreEvent":{"metadata":{"urn":"new","type":"aws:s3/bucket:Bucket","op":"replace","old":{"urn":"old","custom":true,"id":"stale"},"new":{"urn":"new","custom":true,"id":"preview"}}}}`
	got, err := ApplyPreview(as, strings.NewReader(stream))
	if err != nil || len(got) != 1 || got[0].Key != "new" || got[0].ProviderID != "current" || got[0].PlannedAction != "replace" {
		t.Fatalf("revived stale identity or adopted preview ID: %#v %v", got, err)
	}
}
