package pulumicli

import (
	"bytes"
	r "github.com/lambdawalker/go.infra.composer/reporting"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogCLIIsOfflineAndWritesThreeFormats(t *testing.T) {
	dir := t.TempDir()
	scope := filepath.Join(dir, "scope.json")
	os.WriteFile(scope, []byte(`{"application":"test","stack":"test"}`), 0600)
	var out bytes.Buffer
	e := RunCLI([]string{"--scope", scope, "--cache-dir", filepath.Join(dir, "cache"), "--output-dir", filepath.Join(dir, "out")}, []r.Artifact{{Key: "logical", Feature: "capture"}}, &out)
	if e != nil {
		t.Fatal(e)
	}
	for _, ext := range []string{"md", "json", "html"} {
		b, e := os.ReadFile(filepath.Join(dir, "out", "artifacts."+ext))
		if e != nil || !bytes.Contains(b, []byte("logical")) {
			t.Fatalf("%s %v", ext, e)
		}
	}
}
