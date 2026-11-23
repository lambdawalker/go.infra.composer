package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectRememberedAndScoped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	cli := fixture(t, "ocr")
	cli.spec.DefaultProject = "test-host"
	name, local, central, e := cli.projectPaths(t.Context(), root, "test-project", false)
	if e != nil || name != "test-project" || local != filepath.Join(root, ".deploy") || !strings.Contains(central, ".infra-composer") {
		t.Fatal(name, local, central, e)
	}
	if remembered, _, _, err := cli.projectPaths(t.Context(), root, "", false); err != nil || remembered != "test-project" {
		t.Fatalf("saved project = %q, err = %v", remembered, err)
	}
	if _, _, _, e = cli.projectPaths(t.Context(), t.TempDir(), "", true); e == nil {
		t.Fatal("CI accepted an implicit project on a fresh checkout")
	}
	if _, _, _, e = fixture(t, "ocr").projectPaths(t.Context(), t.TempDir(), "test-project", true); e == nil {
		t.Fatal("project collision accepted")
	}
	if _, _, _, e = fixture(t, "ocr").projectPaths(t.Context(), root, "../escape", true); e == nil {
		t.Fatal("scope override accepted")
	}
}
func TestAutomaticStackSelection(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing", false: "new"}[existing], func(t *testing.T) {
			calls := []string{}
			path := filepath.Join(t.TempDir(), "stack.json")
			target := stackRecord{Account: "123", Region: "region", Backend: "s3://bucket/prefix"}
			listing := `[]`
			if existing {
				listing = `[{"name":"organization/card-capture-ocr-bench/ocr-dev"}]`
			}
			e := fixture(t, "ocr").ensureStack(context.Background(), "", "ocr-dev", path, target, true, func(args ...string) error { calls = append(calls, strings.Join(args, " ")); return nil }, func() ([]byte, error) { return []byte(listing), nil })
			if e != nil {
				t.Fatal(e)
			}
			want := "stack init"
			if existing {
				want = "stack select"
			}
			if len(calls) != 1 || !strings.HasPrefix(calls[0], want) {
				t.Fatal(calls)
			}
			if !existing {
				e = fixture(t, "ocr").ensureStack(context.Background(), "", "ocr-dev", path, target, true, func(...string) error { t.Fatal("recreated missing stack"); return nil }, func() ([]byte, error) { return []byte(`[]`), nil })
				if e == nil {
					t.Fatal("lost stack accepted")
				}
			}
		})
	}
}
func TestStackListingFailureNeverInitializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.json")
	e := fixture(t, "ocr").ensureStack(context.Background(), "", "ocr-dev", path, stackRecord{}, true, func(...string) error { t.Fatal("mutation after failed lookup"); return nil }, func() ([]byte, error) { return nil, errors.New("access denied") })
	if e == nil {
		t.Fatal("expected error")
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("success flag written")
	}
}
