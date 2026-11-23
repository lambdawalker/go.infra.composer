package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// A manifest declares one isolated Lambda/Pulumi target. It contains no
// credentials, concrete AWS target settings or executable shell fragments.
type manifest struct {
	Version        int
	Title          string
	Description    string
	Repository     string
	Feature        string
	PulumiProject  string
	PulumiDir      string
	StackPrefix    string
	BucketPrefix   string
	BuildPackage   string
	BuildDir       string
	Memory         int
	StackIdentity  bool
	Outputs        []string
	InputsFile     string
	Inputs         []inputSpec
	Functions      map[string]functionSpec
	ConfigDefaults map[string]string
	GitHub         githubSpec
	inputPlan      []plannedInput
}

type githubSpec struct {
	Workflow          string
	EnvironmentPrefix string
	ManagedPolicy     string
	Prompt            bool
}

type inputSpec struct {
	Help           string
	Name           string // Stable saved record key. Lowercase form is the flag and Pulumi config key.
	Label          string
	Kind           string
	GitHubVariable string
	Compose        *composeSpec
}

type deployCLI struct{ spec manifest }

func readManifest(root string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(filepath.Join(root, "deploy.json"))
	if err != nil {
		return m, fmt.Errorf("read deploy.json at repository root: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&m); err != nil {
		return m, fmt.Errorf("parse deploy.json: %w", err)
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return m, fmt.Errorf("deploy.json must contain one manifest")
	}
	if err = m.validate(); err != nil {
		return m, err
	}
	data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(m.PulumiDir), "Pulumi.yaml"))
	if err != nil {
		return m, fmt.Errorf("read target Pulumi project: %w", err)
	}
	var p struct{ Name string }
	if err = yaml.Unmarshal(data, &p); err != nil || p.Name != m.PulumiProject {
		return m, fmt.Errorf("Pulumi.yaml name must match manifest project %q", m.PulumiProject)
	}
	return m, nil
}

func (m *manifest) validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported deploy.json version %d", m.Version)
	}
	if m.Title == "" || !pattern(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`, m.Repository) {
		return fmt.Errorf("deployment title and GitHub repository are required")
	}
	if !pattern(`^[a-z][a-z0-9-]{0,47}$`, m.PulumiProject) || !pattern(`^[a-z][a-z0-9-]{0,47}$`, m.Feature) ||
		!pattern(`^[a-z][a-z0-9-]{0,15}-$`, m.StackPrefix) || !pattern(`^[a-z][a-z0-9-]{0,29}$`, m.BucketPrefix) {
		return fmt.Errorf("invalid project, feature, stack prefix or bucket prefix")
	}
	if !repoPath(m.PulumiDir) || !repoPath(m.BuildDir) || !strings.HasPrefix(m.BuildDir, "dist/") ||
		!strings.HasPrefix(m.BuildPackage, "./") || !repoPath(strings.TrimPrefix(m.BuildPackage, "./")) {
		return fmt.Errorf("invalid Pulumi or build path; output must be under dist/")
	}
	if m.Memory != 0 && (m.Memory < 128 || m.Memory > 10240) {
		return fmt.Errorf("manifest memory must be 128..10240 MB")
	}
	if len(m.Inputs) > 0 && !pattern(`^[a-z][a-z0-9-]*\.json$`, m.InputsFile) {
		return fmt.Errorf("invalid feature inputs file")
	}
	seenFlags := map[string]bool{}
	seenVariables := map[string]bool{
		"BENCH_PROJECT": true, "BENCH_ACCOUNT": true, "BENCH_REGION": true, "BENCH_BACKEND": true,
		"BENCH_ENVIRONMENT": true, "BENCH_ROLE_ARN": true, "DEPLOY_CONFIG_SHA256": true,
	}
	for _, input := range m.Inputs {
		name := strings.ToLower(input.Name)
		// Reserve all built-in flags and the corresponding Pulumi configuration keys.
		if !pattern(`^[A-Za-z][A-Za-z0-9]*$`, input.Name) || seenFlags[name] ||
			strings.Contains("|environment|action|profile|region|account|backend|approve|project|workers|memory|archive|server|terminal|deploymentproject|deploymentenvironment|version|composeinputs|h|help|", "|"+name+"|") {
			return fmt.Errorf("invalid or duplicate feature flag %q", name)
		}
		if input.Label == "" || !inputKind(input.Kind) {
			return fmt.Errorf("invalid feature input %q", name)
		}
		seenFlags[name] = true
	}
	plan, err := planInputs(m.Inputs, m.Functions)
	if err != nil {
		return err
	}
	for _, input := range plan {
		variable := input.spec.GitHubVariable
		if !input.acceptsInput() {
			if variable != "" {
				return fmt.Errorf("derived input %s cannot have a GitHub variable", input.spec.Name)
			}
			continue
		}
		if !pattern(`^BENCH_[A-Z0-9_]+$`, variable) || seenVariables[variable] {
			return fmt.Errorf("invalid or duplicate GitHub variable %q", variable)
		}
		seenVariables[variable] = true
	}
	for k := range m.ConfigDefaults {
		if !pattern(`^[A-Za-z][A-Za-z0-9]*$`, k) || seenFlags[strings.ToLower(k)] ||
			strings.Contains("|archive|memory|deploymentproject|deploymentenvironment|", "|"+strings.ToLower(k)+"|") {
			return fmt.Errorf("invalid Pulumi configuration default %q", k)
		}
	}
	if !pattern(`^[a-zA-Z0-9_-]+\.ya?ml$`, m.GitHub.Workflow) ||
		!pattern(`^[a-zA-Z0-9_-]+-$`, m.GitHub.EnvironmentPrefix) ||
		(m.GitHub.ManagedPolicy != "" && !pattern(`^[A-Za-z0-9+=,.@_-]{1,128}$`, m.GitHub.ManagedPolicy)) {
		return fmt.Errorf("invalid GitHub workflow, environment prefix or managed policy name")
	}
	m.inputPlan = plan
	return nil
}

func repoPath(s string) bool {
	return filepath.IsLocal(s) && pattern(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`, s) &&
		filepath.ToSlash(filepath.Clean(s)) == s && s != "."
}

func pattern(expr, value string) bool { return regexp.MustCompile(expr).MatchString(value) }
