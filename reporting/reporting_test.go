package reporting

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeHierarchyAndReplacement(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := Report{Scope: Scope{Application: "app"}, Artifacts: []Artifact{{Key: "child", ProviderID: "old", Observation: Observation{Status: Present, Source: "live", ObservedAt: at}}}}
	r := Report{Scope: old.Scope, Artifacts: []Artifact{{Key: "child", Parent: "parent", ProviderID: "new", PlannedAction: "replace"}, {Key: "parent", Parent: "child"}, {Key: "orphan", Parent: "missing"}}}
	got := Merge(r, old)
	if len(got.Artifacts) != 3 || got.Artifacts[0].Observation.Status != Unknown {
		t.Fatalf("replacement inherited observation: %#v", got)
	}
	nodes := Hierarchy(got.Artifacts)
	seen := map[string]bool{}
	var visit func([]Node)
	visit = func(ns []Node) {
		for _, n := range ns {
			if seen[n.Artifact.Key] {
				t.Fatal("cycle")
			}
			seen[n.Artifact.Key] = true
			visit(n.Children)
		}
	}
	visit(nodes)
	if len(seen) != 3 {
		t.Fatalf("lost artifacts: %v", seen)
	}
	r.Scope.Account = "other"
	if Merge(r, old).Artifacts[0].Observation.Status != Unknown {
		t.Fatal("cross-scope observation")
	}
}

func TestCacheAndEscaping(t *testing.T) {
	r := Report{Scope: Scope{Application: "app"}, Artifacts: []Artifact{{Key: "x", Name: "<script>alert(1)</script> | [x](https://bad)", Observation: Observation{Status: Present, Source: "live", ObservedAt: time.Now().UTC()}}}}
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := SaveCache(path, r); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if _, err := LoadCache(path, Scope{Application: "different"}); err == nil {
		t.Fatal("accepted wrong scope")
	}
	got, err := LoadCache(path, r.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.Artifacts[0].Observation.Source != "live" {
		t.Fatal("rewrote evidence")
	}
	var html bytes.Buffer
	if err := WriteHTML(&html, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.String(), "<script>") {
		t.Fatal("HTML injection")
	}
	var md bytes.Buffer
	WriteMarkdown(&md, r)
	if strings.Contains(md.String(), "[x](https://bad)") || strings.Contains(md.String(), "<script>") {
		t.Fatal("Markdown injection")
	}
	var j bytes.Buffer
	if err := WriteJSON(&j, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j.String(), `"schemaVersion": 1`) {
		t.Fatal(j.String())
	}
}

func TestRenderIncludesContextAndReadableUnknownDates(t *testing.T) {
	r := Report{Scope: Scope{Application: "app", Repository: "org/repo", Environment: "prod", Account: "123", Region: "us-west-2", Backend: "s3://state", Stack: "prod"}, Metadata: Metadata{Revision: "abcdef", Outcome: "failed"}, Artifacts: []Artifact{{Key: "a", Feature: "capture", Owner: "org/feature", Type: "aws:s3:Bucket", PlannedAction: "delete"}, {Key: "b", Parent: "a"}}}
	for _, render := range []func(io.Writer, Report) error{WriteHTML, WriteMarkdown} {
		var b bytes.Buffer
		if e := render(&b, r); e != nil {
			t.Fatal(e)
		}
		for _, s := range []string{"org/repo", "us-west-2", "abcdef", "org/feature", "capture", "not checked", "delete"} {
			if !strings.Contains(b.String(), s) {
				t.Fatalf("missing %s in %s", s, b.String())
			}
		}
		if strings.Contains(b.String(), "0001-") {
			t.Fatal("unreadable date")
		}
	}
}

func TestSelectFeatureIncludesExternalParents(t *testing.T) {
	as := []Artifact{{Key: "root", Feature: "host"}, {Key: "child", Feature: "capture", Parent: "root"}, {Key: "other", Feature: "auth"}}
	got := SelectFeature(as, "capture")
	if len(got) != 2 || got[0].Key != "child" || got[1].Key != "root" || got[1].Feature != "host" {
		t.Fatalf("%#v", got)
	}
}

func TestHierarchyKeepsKnownChildrenUnderOrphanParent(t *testing.T) {
	nodes := Hierarchy([]Artifact{{Key: "p", Parent: "missing"}, {Key: "c", Parent: "p"}})
	if len(nodes) != 1 || nodes[0].Artifact.Key != "p" || len(nodes[0].Children) != 1 {
		t.Fatalf("%#v", nodes)
	}
}
