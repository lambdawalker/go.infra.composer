package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PrepareBuild runs a trusted repository Go package, without a shell, before
// compiling Lambda. Inputs include frontend source, lockfiles and the builder;
// outputs are the exact files/directories consumed by the Pulumi program.
type prepareBuildSpec struct {
	Directory string
	Package   string
	Inputs    []string
	Outputs   []string
	Tools     []string
}

func (m manifest) buildRoot(root string) string { return filepath.Join(root, m.BuildWorkingDir) }
func (m manifest) stackName(project, environment string) string {
	return strings.ReplaceAll(m.StackPrefix, "{project}", project) + environment
}
func (m manifest) validEnvironment(environment string) bool {
	return pattern(`^[a-z][a-z0-9-]{0,30}$`, environment) && (m.AllowProduction || (environment != "prod" && environment != "production"))
}
func (m manifest) validateBuild() error {
	if m.BuildWorkingDir != "" && !repoPath(m.BuildWorkingDir) {
		return fmt.Errorf("invalid BuildWorkingDir")
	}
	if p := m.PrepareBuild; p != nil {
		if !repoPath(p.Directory) || (p.Package != "." && (!strings.HasPrefix(p.Package, "./") || !repoPath(strings.TrimPrefix(p.Package, "./")))) || len(p.Inputs) == 0 || len(p.Outputs) == 0 {
			return fmt.Errorf("invalid PrepareBuild package, inputs or outputs")
		}
		for _, tool := range p.Tools {
			if tool != "node" && tool != "npm" {
				return fmt.Errorf("unsupported preparation tool %q", tool)
			}
		}
		for _, path := range append(append([]string{}, p.Inputs...), p.Outputs...) {
			if !repoPath(path) {
				return fmt.Errorf("invalid PrepareBuild path %q", path)
			}
		}
	}
	return nil
}

// Hash names and bytes with JSON boundaries. Reject symlinks and special files
// rather than silently reading outside the checkout. Generated/cache directories
// are skipped only when collecting inputs, never when verifying outputs.
func contentDigest(root string, paths []string, inputs bool) (string, error) {
	files := map[string]string{}
	for _, name := range paths {
		path := filepath.Join(root, filepath.FromSlash(name))
		// WalkDir does not follow a symlink root, but also reject symlink ancestors.
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path outside checkout")
		}
		parent := root
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			parent = filepath.Join(parent, part)
			info, err := os.Lstat(parent)
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("symlink build path: %s", name)
			}
		}
		count := 0
		err = filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if inputs && (d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == ".git" || d.Name() == ".deploy") {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("nonregular build input/output: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			key, _ := filepath.Rel(root, path)
			files[filepath.ToSlash(key)] = hex.EncodeToString(sum[:])
			count++
			return nil
		})
		if err != nil {
			return "", err
		}
		if count == 0 {
			return "", fmt.Errorf("empty build input/output: %s", name)
		}
	}
	data, _ := json.Marshal(files)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (cli deployCLI) preparationInput(ctx context.Context, root string) (string, error) {
	p := cli.spec.PrepareBuild
	source, err := contentDigest(root, p.Inputs, true)
	if err != nil {
		return "", err
	}
	builder, err := buildFingerprint(ctx, filepath.Join(root, p.Directory), p.Package)
	if err != nil {
		return "", err
	}
	versions := map[string]string{}
	for _, tool := range p.Tools {
		args := []string{tool, "--version"}
		if runtime.GOOS == "windows" && tool == "npm" {
			args = []string{"cmd.exe", "/d", "/c", "npm.cmd", "--version"}
		}
		version, err := output(ctx, args...)
		if err != nil {
			return "", fmt.Errorf("read %s version: %w", tool, err)
		}
		versions[tool] = string(version)
	}
	// Include the declarative recipe so changing inputs/outputs invalidates cache.
	return canonicalHash(struct {
		Version         int
		Recipe          *prepareBuildSpec
		Source, Builder string
		Versions        map[string]string
	}{1, p, source, builder, versions}), nil
}
func (cli deployCLI) prepareBuild(ctx context.Context, root, out string, force bool) error {
	p := cli.spec.PrepareBuild
	if p == nil {
		return nil
	}
	before, err := cli.preparationInput(ctx, root)
	if err != nil {
		return err
	}
	marker := filepath.Join(out, "prepare.json")
	var old struct{ Input, Output string }
	if data, err := os.ReadFile(marker); err == nil {
		_ = json.Unmarshal(data, &old)
	}
	if !force && old.Input == before {
		if output, err := contentDigest(root, p.Outputs, false); err == nil && output == old.Output {
			wizard(ctx).note("Build: reusing verified prepared artifacts")
			return nil
		}
	}
	if err = command(ctx, filepath.Join(root, p.Directory), "go", "run", p.Package); err != nil {
		return fmt.Errorf("prepare deployment artifacts: %w", err)
	}
	after, err := cli.preparationInput(ctx, root)
	if err != nil {
		return err
	}
	if before != after {
		return fmt.Errorf("preparation inputs changed during build; retry")
	}
	output, err := contentDigest(root, p.Outputs, false)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(struct{ Input, Output string }{after, output})
	return atomicWrite(marker, data)
}
func (cli deployCLI) deploymentDigest(ctx context.Context, root string) (string, error) {
	lambda, err := buildFingerprint(ctx, cli.spec.buildRoot(root), cli.spec.BuildPackage)
	if err != nil {
		return "", err
	}
	infra, err := contentDigest(root, []string{cli.spec.PulumiDir, "deploy.json"}, true)
	if err != nil {
		return "", err
	}
	parts := []string{lambda, infra, canonicalHash(cli.spec)}
	if p := cli.spec.PrepareBuild; p != nil {
		input, err := cli.preparationInput(ctx, root)
		if err != nil {
			return "", err
		}
		output, err := contentDigest(root, p.Outputs, false)
		if err != nil {
			return "", err
		}
		parts = append(parts, input, output)
	}
	return canonicalHash(parts), nil
}
