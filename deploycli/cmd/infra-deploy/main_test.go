package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAtomicWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings")
	if e := atomicWrite(p, []byte("first")); e != nil {
		t.Fatal(e)
	}
	if e := atomicWrite(p, []byte("second")); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "second" {
		t.Fatal(string(b), e)
	}
}

func TestBuildCacheDetectsTampering(t *testing.T) {
	// Use a fake compiler to exercise publication/cache behavior without cloud calls.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake compiler")
	}
	root := t.TempDir()
	bin := t.TempDir()
	fake := filepath.Join(bin, "go")
	script := `#!/bin/sh
if [ "$1" = list ]; then printf '{"Dir":"%s","GoFiles":["source.go"],"Module":{"GoMod":"%s/go.mod"}}' "$PWD" "$PWD"; exit; fi
if [ "$1" = env ]; then echo '{}'; exit; fi
if [ "$1" = version ]; then echo 'go version go1.27.3 linux/amd64'; exit; fi
while [ $# -gt 0 ]; do if [ "$1" = -o ]; then shift; printf binary > "$1"; exit; fi; shift; done
exit 1
`
	if e := os.WriteFile(fake, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example"), 0600)
	os.WriteFile(filepath.Join(root, "source.go"), []byte("package old"), 0600)
	archive, e := fixture(t, "ocr").build(context.Background(), root, false)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.Stat(archive)
	if _, e = fixture(t, "ocr").build(context.Background(), root, false); e != nil {
		t.Fatal(e)
	}
	after, _ := os.Stat(archive)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged rebuilt")
	}
	os.WriteFile(archive, []byte("tampered"), 0600)
	if _, e = fixture(t, "ocr").build(context.Background(), root, false); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(archive)
	if string(b) == "tampered" {
		t.Fatal("tampered archive reused")
	}
	os.WriteFile(filepath.Join(root, "source.go"), []byte("package example"), 0600)
	old, _ := os.ReadFile(archive + ".json")
	if _, e = fixture(t, "ocr").build(context.Background(), root, false); e != nil {
		t.Fatal(e)
	}
	next, _ := os.ReadFile(archive + ".json")
	if string(old) == string(next) {
		t.Fatal("source fingerprint unchanged")
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--input-child" {
		var b [256]byte
		for {
			n, e := os.Stdin.Read(b[:])
			if n > 0 {
				os.Stdout.Write(b[:n])
			}
			if e != nil {
				os.Exit(0)
			}
		}
	}

	os.Exit(m.Run())
}
func TestTimedChoice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("console key helper requires Windows terminal")
	}
	for _, tc := range []struct {
		name, input     string
		def, want, fail bool
	}{
		{"yes", "y", false, true, false}, {"no", "n", true, false, false}, {"enter-default-no", "\n", false, false, false}, {"enter-default-yes", "\n", true, true, false}, {"eof", "", true, false, true}, {"escape", "\x1b", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close()
			old := os.Stdin
			os.Stdin = r
			defer func() { os.Stdin = old }()
			w.Write([]byte(tc.input))
			w.Close()
			got, e := timedChoice(tc.def)
			if got != tc.want || (e != nil) != tc.fail {
				t.Fatal(got, e)
			}
		})
	}
}
func TestTimedChoiceTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pipe")
	}
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	previous := countdownStep
	countdownStep = 20 * time.Millisecond
	defer func() { countdownStep = previous }()
	for _, def := range []bool{false, true} {
		got, e := timedChoice(def)
		if e != nil || got != def {
			t.Fatal(got, e)
		}
	}
}
func TestBootstrapResume(t *testing.T) {
	dir := t.TempDir()
	creates := 0
	fail := true
	run := func(args ...string) error {
		if args[0] == "create-bucket" {
			creates++
		}
		if args[0] == "put-bucket-versioning" && fail {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, e := bootstrapWith(context.Background(), dir, "123456789012", "us-east-2", "ocr-bench-state", run); e == nil {
		t.Fatal("expected failure")
	}
	fail = false
	name, e := bootstrapWith(context.Background(), dir, "123456789012", "us-east-2", "ocr-bench-state", run)
	if e != nil || name == "" || creates != 1 {
		t.Fatal(name, e, creates)
	}
	if _, e = bootstrapWith(context.Background(), dir, "999999999999", "us-east-2", "ocr-bench-state", run); e == nil {
		t.Fatal("scope mismatch accepted")
	}
}

func TestBootstrapRejectsInvalidNameBeforeAWSOrReceipt(t *testing.T) {
	dir := t.TempDir()
	_, err := bootstrapWith(context.Background(), dir, "123456789012", "us-east-2", "amzn-s3-demo-email", func(...string) error {
		t.Fatal("invalid bucket reached AWS")
		return nil
	})
	if err == nil {
		t.Fatal("accepted reserved bucket prefix")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected bootstrap receipt: %v, %v", entries, err)
	}
}
