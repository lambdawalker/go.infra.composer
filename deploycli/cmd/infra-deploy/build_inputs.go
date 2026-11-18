package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Ask Go for the actual dependency graph, including local replacements and
// embedded assets. File bytes, not timestamps or the Git revision, are inputs.
func buildFingerprint(ctx context.Context, root, buildPackage string) (string, error) {
	c := exec.CommandContext(ctx, "go", "list", "-deps", "-json", "-tags", "lambda.norpc", buildPackage)
	c.Dir = root
	c.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	raw, e := c.Output()
	if e != nil {
		return "", fmt.Errorf("resolve build inputs: %w", e)
	}
	files := map[string]string{}
	add := func(path string) error {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		h := sha256.Sum256(b)
		files[filepath.ToSlash(path)] = hex.EncodeToString(h[:])
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	count := 0
	for {
		var p struct {
			Dir                                                      string
			GoFiles, CgoFiles, SFiles, SysoFiles, EmbedFiles, HFiles []string
			Module                                                   *struct{ GoMod string }
		}
		e = d.Decode(&p)
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
		count++
		for _, group := range [][]string{p.GoFiles, p.CgoFiles, p.SFiles, p.SysoFiles, p.EmbedFiles, p.HFiles} {
			for _, name := range group {
				if e = add(filepath.Join(p.Dir, name)); e != nil {
					return "", e
				}
			}
		}
		if p.Module != nil && p.Module.GoMod != "" {
			if e = add(p.Module.GoMod); e != nil {
				return "", e
			}
			sum := filepath.Join(filepath.Dir(p.Module.GoMod), "go.sum")
			if _, e = os.Stat(sum); e == nil {
				if e = add(sum); e != nil {
					return "", e
				}
			}
		}
	}
	if count == 0 {
		return "", fmt.Errorf("empty Go dependency graph")
	}
	version, e := output(ctx, "go", "version")
	if e != nil {
		return "", e
	}
	env := exec.CommandContext(ctx, "go", "env", "-json", "GOFLAGS", "GOEXPERIMENT", "GOARM64", "GOTOOLCHAIN", "GOWORK")
	env.Dir = root
	environment, e := env.Output()
	if e != nil {
		return "", e
	}
	var settings map[string]string
	if e = json.Unmarshal(environment, &settings); e != nil {
		return "", e
	}
	if work := settings["GOWORK"]; work != "" && work != "off" {
		if e = add(work); e != nil {
			return "", e
		}
		if _, e = os.Stat(work + ".sum"); e == nil {
			if e = add(work + ".sum"); e != nil {
				return "", e
			}
		}
	}
	manifest := struct {
		Schema, Recipe, Root, Toolchain, Package string
		Environment, Files                       map[string]string
	}{"3", "linux/arm64/cgo0/trimpath/lambda.norpc/zip-deflate-mode0755", root, string(version), buildPackage, settings, files}
	b, e := json.Marshal(manifest)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
