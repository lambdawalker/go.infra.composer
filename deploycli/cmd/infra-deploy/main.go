// infra-deploy orchestrates a standalone feature deployment.
package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
	"github.com/lambdawalker/go.infra.composer/discovery"
)

type Settings struct {
	Version                                        int
	Environment, Profile, Region, Account, Backend string
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(error); ok && errors.Is(err, errQuit) {
				note("Wizard closed.")
				return
			}
			fmt.Fprintln(os.Stderr, r)
			os.Exit(1)
		}
	}()
	if len(os.Args) == 2 && os.Args[1] == "--input-child" {
		var b [256]byte
		for {
			n, err := os.Stdin.Read(b[:])
			if n > 0 {
				_, _ = os.Stdout.Write(b[:n])
			}
			if err != nil {
				return
			}
		}
	}

	root, e := os.Getwd()
	if e == nil {
		var spec manifest
		spec, e = readManifest(root)
		if e == nil {
			e = (deployCLI{spec}).run()
		}
	}
	if e != nil {
		if errors.Is(e, errQuit) {
			note("Wizard closed.")
			return
		}
		if errors.Is(e, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "\n  "+color("Stopped", "1;31")+"  "+e.Error())
		os.Exit(1)
	}
}

func command(ctx context.Context, dir string, args ...string) error {
	c := toolCommand(ctx, args...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	capture := captureDiagnostics(ctx, c)
	err := c.Run()
	capture(err)
	return err
}
func output(ctx context.Context, args ...string) ([]byte, error) {
	c := toolCommand(ctx, args...)
	c.Stderr = os.Stderr
	capture := captureDiagnostics(ctx, c)
	result, err := c.Output()
	capture(err)
	return result, err
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
func vault(ctx context.Context, path, pass string) (string, error) {
	b, e := os.ReadFile(path)
	fresh := os.IsNotExist(e)
	if e != nil && !fresh {
		return "", e
	}
	if fresh {
		if ui := wizard(ctx); ui.browser == nil {
			again, e := ui.secret("Confirm vault passphrase:")
			if e != nil {
				return "", e
			}
			if again != pass {
				return "", errors.New("passphrases differ")
			}
		}
		b = make([]byte, 20)
		copy(b, []byte("OCV1"))
		if _, e = rand.Read(b[4:]); e != nil {
			return "", e
		}
	}
	if len(b) < 20 || string(b[:4]) != "OCV1" {
		return "", errors.New("invalid vault; restore backup")
	}
	g, e := vaultCipher(pass, b[4:20])
	if e != nil {
		return "", e
	}
	aad := []byte(path)
	if !fresh {
		if len(b) < 20+g.NonceSize() {
			return "", errors.New("corrupt vault")
		}
		plain, e := g.Open(nil, b[20:20+g.NonceSize()], b[20+g.NonceSize():], aad)
		if e != nil {
			return "", errors.New("vault unlock failed; existing file preserved")
		}
		defer clear(plain)
		return string(plain), nil
	}
	pulumiPass, e := wizard(ctx).secret("Pulumi passphrase (use the existing passphrase for an existing stack):")
	if e != nil {
		return "", e
	}
	if pulumiPass == "" {
		return "", errors.New("Pulumi passphrase required")
	}
	again, e := wizard(ctx).secret("Confirm Pulumi passphrase:")
	if e != nil {
		return "", e
	}
	if again != pulumiPass {
		return "", errors.New("passphrases differ")
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	b = append(b, nonce...)
	b = g.Seal(b, nonce, []byte(pulumiPass), aad)
	return pulumiPass, atomicWrite(path, b)
}
func confirm(ctx context.Context, target ...string) error {
	yes, err := wizard(ctx).choose("Deploy the previewed changes? [Y/n]", "Apply saved plan", true, target...)
	if err != nil {
		return err
	}
	if !yes {
		return wizard(ctx).declined("deployment cancelled")
	}
	return nil
}

func (cli deployCLI) build(ctx context.Context, root string, force bool) (string, error) {
	out := filepath.Join(root, cli.spec.BuildDir)
	if e := os.MkdirAll(out, 0700); e != nil {
		return "", e
	}
	unlock, e := operationLock(filepath.Join(out, "build.lock"))
	if e != nil {
		return "", e
	}
	defer unlock()
	fingerprint, e := buildFingerprint(ctx, root, cli.spec.BuildPackage)
	if e != nil {
		return "", e
	}

	archive := filepath.Join(out, "lambda.zip")
	marker := archive + ".json"
	var old struct{ Input, Artifact string }
	if b, e := os.ReadFile(marker); e == nil {
		_ = json.Unmarshal(b, &old)
	}
	if b, e := os.ReadFile(archive); e == nil && !force {
		sum := sha256.Sum256(b)
		if old.Input == fingerprint && old.Artifact == hex.EncodeToString(sum[:]) {
			wizard(ctx).note("Build: reusing verified Lambda archive")
			return archive, nil
		}
	}
	temp, e := os.MkdirTemp(out, "build-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(temp)
	binary := filepath.Join(temp, "bootstrap")
	c := exec.CommandContext(ctx, "go", "build", "-trimpath", "-tags", "lambda.norpc", "-o", binary, cli.spec.BuildPackage)
	c.Dir = root
	c.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	capture := captureDiagnostics(ctx, c)
	e = c.Run()
	capture(e)
	if e != nil {
		return "", e
	}
	f, e := os.Create(filepath.Join(temp, "lambda.zip"))
	if e != nil {
		return "", e
	}
	z := zip.NewWriter(f)
	header := &zip.FileHeader{Name: "bootstrap", Method: zip.Deflate}
	header.SetMode(0755)
	w, e := z.CreateHeader(header)
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(binary)
	if e != nil {
		return "", e
	}
	if _, e = w.Write(b); e != nil {
		return "", e
	}
	if e = z.Close(); e != nil {
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	after, e := buildFingerprint(ctx, root, cli.spec.BuildPackage)
	if e != nil {
		return "", e
	}
	if after != fingerprint {
		return "", errors.New("build inputs changed during compilation; retry")
	}
	b, e = os.ReadFile(f.Name())
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	if e = atomicWrite(archive, b); e != nil {
		return "", e
	}
	manifest, _ := json.Marshal(struct{ Input, Artifact string }{fingerprint, hex.EncodeToString(sum[:])})
	return archive, atomicWrite(marker, manifest)
}
func (cli deployCLI) run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return cli.runWizard(ctx, os.Args[1:], false)
}

func (cli deployCLI) runWizard(ctx context.Context, args []string, showActions bool) (runErr error) {
	ui := wizard(ctx)
	fs := flag.NewFlagSet("infra-deploy", flag.ContinueOnError)
	env := fs.String("environment", "", "isolated environment")
	var discoveryURL, discoveryRegion string
	if cli.spec.Discovery != nil {
		fs.StringVar(&discoveryURL, "discovery-index", os.Getenv("INFRA_DISCOVERY_INDEX_URL"), "HTTPS environment discovery index URL")
		fs.StringVar(&discoveryRegion, "discovery-region", os.Getenv("INFRA_DISCOVERY_INDEX_REGION"), "AWS region hosting the discovery index")
	}
	action := fs.String("action", "", "setup, build, clean-cache, preview, deploy, status, destroy, destroy-all, github-setup, github-teardown, report (default: deploy interactively, preview in CI)")
	profile := fs.String("profile", "", "AWS SSO profile (omit in CI)")
	region := fs.String("region", "", "AWS region")
	account := fs.String("account", "", "expected AWS account")
	backend := fs.String("backend", "", "s3://bucket/dedicated-prefix")
	indexBackend := fs.String("index-backend", "", "existing S3 state URL for the shared discovery index (destroy-all)")
	ci := fs.Bool("non-interactive", false, "complete explicit inputs required")
	server := fs.Bool("server", false, "open the interactive wizard in a local web browser")
	terminal := fs.Bool("terminal", false, "use the terminal wizard without asking which interface to open")
	noOpen := fs.Bool("no-open", false, "print the browser URL without opening it (requires --server)")
	approve := fs.Bool("approve", false, "explicit apply approval")
	project := fs.String("project", "", "deployment project name (saved after first run)")
	_ = fs.Bool("init-stack", false, "deprecated: stacks are detected automatically")
	exportPath := fs.String("export-settings", "", "export allowlisted nonsecret settings and exit before AWS access")
	importPath := fs.String("import-settings", "", "import compatible nonsecret settings; never import ownership/success")
	workers := fs.Int("workers", 2, "maximum concurrent prerequisite checks (1..8)")
	force := fs.Bool("force-rebuild", false, "ignore build cache")
	reconfigure := fs.Bool("reconfigure", false, "ask for saved settings again")
	adoptLegacy := fs.Bool("adopt-legacy-stack", false, "explicitly review adopting identity metadata for an existing legacy stack")
	refreshReport := fs.Bool("refresh-report", false, "verify report presence with read-only refresh")
	memory := new(int)
	if cli.spec.Memory != 0 {
		memory = fs.Int("memory", cli.spec.Memory, "Lambda memory MB")
	}
	inputs := cli.spec.inputFlags(fs)
	githubRole := fs.String("github-role", "", "compatible OIDC role ARN; omitted derives a project/environment role")
	githubPolicy := fs.String("github-policy", "", "reviewed least-privilege IAM policy JSON, required to create a role")
	githubSecretVersion := fs.String("github-secret-version", "", "nonsecret rotation identifier; change to redeliver secrets")
	skipGitHub := fs.Bool("skip-github", false, "skip optional GitHub reconciliation for this local run")
	forceReconcile := fs.Bool("force-reconcile", false, "reconcile managed GitHub settings and redeliver the secret")
	githubRepo := fs.String("github-repository", "", "verified repository")
	githubEnv := fs.String("github-environment", "", "GitHub deployment environment")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if os.Getenv("CI") != "" {
		*ci = true
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
	if *server && *terminal {
		return errors.New("--server and --terminal cannot be combined")
	}
	if *noOpen && !*server {
		return errors.New("--no-open requires --server")
	}
	if *ci && *server {
		return errors.New("--server requires an interactive run")
	}
	actionExplicit := *action != ""
	*action = defaultAction(*action, *ci)
	if *ci && *action == "deploy" && !*approve {
		return errors.New("noninteractive deploy requires explicit --action deploy --approve")
	}
	if *ci && (*profile != "" || os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_DEFAULT_PROFILE") != "") {
		return errors.New("CI must use OIDC/assumed-role credentials, not --profile")
	}

	if *refreshReport && *action != "report" {
		return errors.New("--refresh-report requires --action report")
	}
	allowedActions := map[string]bool{"setup": true, "build": true, "clean-cache": true, "preview": true, "deploy": true, "status": true, "destroy": true, "destroy-all": true, "github-setup": true, "github-teardown": true, "report": true}
	if !allowedActions[*action] {
		return errors.New("unknown action")
	}
	if ui.browser == nil && !*ci && !*terminal && !*server && *exportPath == "" &&
		*action != "build" && *action != "clean-cache" {
		serverSpecified := false
		fs.Visit(func(f *flag.Flag) {
			serverSpecified = serverSpecified || f.Name == "server"
		})
		if !serverSpecified {
			web, err := chooseWizardMode(ctx)
			if err != nil {
				return err
			}
			*server = web
		}
	}
	if *server && ui.browser == nil {
		return wizardweb.Serve(ctx, cli.spec.Title, !*noOpen, func(ctx context.Context, session *wizardweb.Session, showActions bool) error {
			return cli.runWizard(context.WithValue(ctx, wizardKey{}, session), args, showActions)
		})
	}
	if showActions {
		operation, err := ui.selectAction(false)
		if err != nil {
			return err
		}
		*action = operation
		*exportPath, *importPath = "", ""
		*refreshReport, *adoptLegacy, *forceReconcile = false, false, false
	} else if ui.browser != nil && (actionExplicit || *exportPath != "") {
		ui.browser.Action(*action)
	}
	ui.phase("settings", "Configure deployment")
	if ui.browser == nil {
		banner(cli.spec, *action, *env)
	}
	if *action == "clean-cache" {
		root, e := os.Getwd()
		if e != nil {
			return e
		}
		dir := filepath.Join(root, cli.spec.BuildDir)
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		unlock, e := operationLock(filepath.Join(dir, "build.lock"))
		if e != nil {
			return e
		}
		defer unlock()
		for _, name := range []string{"lambda.zip", "lambda.zip.json"} {
			if e = os.Remove(filepath.Join(dir, name)); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
		return nil
	}
	if *action == "build" {
		ui.phase("prepare", "Build Lambda")
		root, e := os.Getwd()
		if e != nil {
			return e
		}
		_, e = cli.build(ctx, root, *force)
		return e
	}
	root, e := os.Getwd()
	if e != nil {
		return e
	}
	var vaultPassphrase string
	if *exportPath == "" {
		if *ci && os.Getenv("PULUMI_CONFIG_PASSPHRASE") == "" {
			return errors.New("PULUMI_CONFIG_PASSPHRASE secret required")
		}
		if !*ci {
			if ui.browser != nil {
				vaultPassphrase, e = ui.browser.VaultPassphrase(ctx)
			} else {
				vaultPassphrase, e = ui.secret("Local vault passphrase")
			}
			if e != nil {
				return e
			}
			if vaultPassphrase == "" {
				return errors.New("vault passphrase required")
			}
		}
	}
	if ui.browser != nil && !showActions && !actionExplicit && *exportPath == "" {
		*action, e = ui.selectAction(true)
		if e != nil {
			return e
		}
	}
	if *action == "destroy-all" {
		return cli.destroyAll(ctx, *project, *indexBackend, *ci)
	}
	if !*ci && *env == "" && (*action == "destroy" || *action == "github-teardown") {
		*env, e = ui.input("Environment to remove (dev, qa, or custom)", "", "")
		if e != nil {
			return e
		}
		if *env == "" {
			return errors.New("select an environment to remove")
		}
	}
	if *ci && *action == "github-setup" {
		return errors.New("GitHub setup requires interactive review")
	}
	projectName, local, central, e := cli.projectPaths(ctx, root, *project, *ci)
	if e != nil {
		return e
	}
	// Pulumi writes shared workspace files even across environments.
	workspaceUnlock, e := operationLock(filepath.Join(local, "workspace.lock"))
	if e != nil {
		return e
	}
	defer workspaceUnlock()
	ui.detail("Project", projectName)
	var projectFlags struct {
		Version     int
		Environment string
	}
	projectFlagsPath := filepath.Join(local, "flags.json")
	if b, err := os.ReadFile(projectFlagsPath); err == nil {
		if json.Unmarshal(b, &projectFlags) != nil || projectFlags.Version > 1 {
			return errors.New("invalid .deploy/flags.json")
		}
		if *env == "" {
			*env = projectFlags.Environment
		}
	}

	if *env == "" {
		if *ci {
			return errors.New("--environment required")
		}
		names := []string{"dev", "qa"}
		if entries, err := os.ReadDir(filepath.Join(central, "environments")); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && entry.Name() != "dev" && entry.Name() != "qa" {
					names = append(names, entry.Name())
				}
			}
		}
		*env = ui.prompt("Environment ("+strings.Join(names, ", ")+", or custom test name)", "dev")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`).MatchString(*env) || *env == "prod" || *env == "production" {
		return errors.New("use a validated non-production test environment")
	}
	if _, e = os.Stat(filepath.Join(root, "go.mod")); e != nil {
		return errors.New("run from repository root or use deploy launcher")
	}
	private := filepath.Join(central, "environments", *env)
	localEnv := filepath.Join(local, "environments", *env)
	if e = privateDirectory(localEnv); e != nil {
		return e
	}

	if e = privateDirectory(private); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(private, "operation.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("operation lock exists; verify no active process before manually removing it")
	}
	lock.Close()
	defer os.Remove(lock.Name())
	if cli.spec.Memory != 0 {
		var deploymentFlags struct{ Version, Memory int }
		memoryExplicit := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "memory" {
				memoryExplicit = true
			}
		})
		if *ci && !memoryExplicit {
			return errors.New("--memory required in CI; do not infer reviewed deployment configuration")
		}
		deploymentFlagsPath := filepath.Join(private, "flags.json")
		if b, err := os.ReadFile(deploymentFlagsPath); err == nil {
			if json.Unmarshal(b, &deploymentFlags) != nil || deploymentFlags.Version > 1 {
				return errors.New("invalid deployment flags")
			}
			if !memoryExplicit && deploymentFlags.Memory >= 128 && deploymentFlags.Memory <= 10240 {
				*memory = deploymentFlags.Memory
			}
		}
		if *memory < 128 || *memory > 10240 {
			return errors.New("memory must be 128..10240 MB")
		}
	}
	saved := Settings{Version: 1, Environment: *env}
	path := filepath.Join(private, "settings.json")
	legacyHome, _ := os.UserConfigDir()
	legacy := filepath.Join(legacyHome, "attestra", cli.spec.PulumiProject, *env)
	if _, err := os.Stat(path); os.IsNotExist(err) && projectName == cli.spec.PulumiProject {
		if previous, err := os.ReadFile(filepath.Join(legacy, "settings.json")); err == nil {
			if e = atomicWrite(path, previous); e != nil {
				return e
			}
			ui.note("Imported previous deployment settings")
		}
	}

	if b, e := os.ReadFile(path); e == nil {
		if json.Unmarshal(b, &saved) != nil || saved.Version != 1 || saved.Environment != *env {
			return errors.New("invalid settings")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if *exportPath != "" {
		if saved.Account == "" {
			return errors.New("no saved settings to export")
		}
		return exportSettings(*exportPath, projectName, saved)
	}
	var credentials, submittedCredentials providerCredentials
	credentialPath := filepath.Join(localEnv, "credentials.bin")
	if !*ci {
		legacyVault := ""
		if projectName == cli.spec.PulumiProject {
			legacyVault = filepath.Join(legacy, "vault.bin")
		}
		pass, err := loadProjectVault(ctx, filepath.Join(localEnv, "vault.bin"), legacyVault, vaultPassphrase)
		if err == nil {
			credentials, err = readCredentials(credentialPath, vaultPassphrase)
		}
		if err != nil {
			if ui.browser != nil {
				ui.browser.ForgetVault()
			}
			return err
		}
		submittedCredentials = credentials
		restorePassphrase, err := setRunEnv("PULUMI_CONFIG_PASSPHRASE", pass)
		if err != nil {
			return err
		}
		defer restorePassphrase()
		if ui.browser != nil {
			ui.browser.Protect(pass, credentials.Cloudflare, credentials.GitHub)
		}
	}
	if *importPath != "" {
		imported, err := importSettings(*importPath, projectName, *env)
		if err != nil {
			return err
		}
		if err = verifySavedTarget(saved, imported.Account, imported.Region, imported.Backend); err != nil {
			return err
		}
		saved = imported
		ui.note("Using compatible imported nonsecret settings; remote discovery remains required")
	}
	if e = verifySavedTarget(saved, *account, *region, *backend); e != nil {
		return e
	}

	needsInputs := *action == "setup" || *action == "preview" || *action == "deploy" || *action == "github-setup"
	canCreateBucket := *action == "setup" || *action == "preview" || *action == "deploy"
	if !*ci {
		if *account == "" {
			*account = saved.Account
		}
		if *backend == "" {
			*backend = saved.Backend
		}
	}
	options := githubOptions{target: cli.spec, Root: root, Role: *githubRole, Repository: *githubRepo, Environment: *githubEnv, Project: projectName, Policy: *githubPolicy, SecretVersion: *githubSecretVersion, Force: *forceReconcile, Private: private}
	var reconcile bool
	offeredGitHub := !*ci && (*action == "github-setup" || (cli.spec.GitHub.Prompt && !*skipGitHub && (*action == "setup" || *action == "deploy")))
	var profiles []wizardweb.Option
	var profileListError error
	var who awsIdentity
	profileDefault := ""
	if !*ci {
		profiles, profileListError = listAWSProfiles(ctx)
		if profileListError != nil && ui.browser == nil {
			return profileListError
		}
		profileDefault = defaultAWSProfile(profiles)
		if profileDefault == "" && ui.browser == nil {
			return errors.New("no AWS profiles; run aws configure sso first")
		}
	}

	if ui.browser != nil && !*ci {
		form := deploymentForm{
			project: projectName, action: *action, root: root, saved: saved, profiles: profiles, canConfigureSSO: profileSetupTerminal(),
			target:          Settings{Version: 1, Environment: *env, Profile: *profile, Region: *region, Account: *account, Backend: *backend},
			canCreateBucket: canCreateBucket, memory: *memory, hasMemory: cli.spec.Memory != 0,
			credentials: credentials,
		}
		if form.target.Profile == "" {
			form.target.Profile = profileDefault
			for _, option := range profiles {
				if option.Value == saved.Profile {
					form.target.Profile = saved.Profile
					break
				}
			}
		}
		if form.target.Region == "" {
			form.target.Region = saved.Region
			if form.target.Region == "" {
				form.target.Region = "us-east-2"
			}
		}
		if *account == "" {
			form.target.Account = "auto"
		}
		if *backend == "" && canCreateBucket {
			form.target.Backend = "new"
		}
		if needsInputs {
			form.inputs = inputs
			form.defaults, e = inputs.defaults(cli.spec.InputsFile, private)
			if e != nil {
				return e
			}
		}
		if offeredGitHub {
			previous, err := readGitHubSettings(private)
			if err != nil {
				return err
			}
			github := options
			github.fillDefaults(previous, *env)
			form.github = &github
			form.configureGitHub = previous.Repository != "" || *reconfigure
		}
		var initialIssues map[string]string
		if profileListError != nil {
			initialIssues = map[string]string{"profile": profileListError.Error()}
		}
		if e = form.collect(ctx, initialIssues, func(target Settings) (string, error) {
			var field string
			var err error
			who, field, err = checkConnection(ctx, root, target, false, *workers, target.Backend != "new")
			return field, err
		}); e != nil {
			return e
		}
		*profile, *region, *account, *backend = form.target.Profile, form.target.Region, form.target.Account, form.target.Backend
		*memory, reconcile = form.memory, form.configureGitHub
		inputs = form.inputs
		submittedCredentials = form.credentials
		if form.github != nil {
			options = *form.github
		}
	} else {
		choose := func(v *string, prior, label, def string) error {
			if *v != "" {
				return nil
			}
			if !*ci && prior != "" && !*reconfigure {
				*v = prior
				ui.note(label + ": using saved setting")
				return nil
			}
			if *ci {
				return fmt.Errorf("%s required in CI", label)
			}
			if prior != "" {
				def = prior
			}
			*v = ui.prompt(label, def)
			return nil
		}
		if !*ci {
			if e = choose(profile, saved.Profile, "AWS SSO profile", profileDefault); e != nil {
				return e
			}
		}
		for _, v := range []struct {
			p               *string
			old, label, def string
		}{{region, saved.Region, "region", "us-east-2"}} {
			if e = choose(v.p, v.old, v.label, v.def); e != nil {
				return e
			}
		}
		if !*ci {
			if *backend == "" {
				def := ""
				label := "S3 state URL (s3://bucket/prefix)"
				if canCreateBucket {
					def = "new"
					label = "S3 state URL (s3://bucket/prefix or new)"
				}
				for {
					*backend = ui.prompt(label, def)
					if validBackend(*backend) || (canCreateBucket && *backend == "new") {
						break
					}
					ui.note("Enter an S3 state URL with a bucket and a prefix.")
				}
			}
			if offeredGitHub {
				reconcile, e = options.collectGitHub(ctx, *env, *action == "github-setup" || *reconfigure)
				if e != nil {
					return e
				}
			}
		}
		if needsInputs {
			if e = inputs.collect(ctx, cli.spec.InputsFile, private, *env, *ci, *reconfigure, ui.input); e != nil {
				return e
			}
		}
		if !*ci {
			regionField := textReviewField(ctx, "AWS region", region, "", validRegion)
			accountField := textReviewField(ctx, "AWS account", account, "auto", func(s string) bool {
				return s == "auto" || pattern(`^\d{12}$`, s)
			})
			stateField := textReviewField(ctx, "S3 state URL", backend, "", func(s string) bool {
				return validBackend(s) || (canCreateBucket && s == "new")
			})
			if saved.Account != "" {
				// These values identify the existing stack.
				regionField.edit, accountField.edit, stateField.edit = nil, nil, nil
			}
			rows := []reviewField{
				{label: "Project", value: func() string { return projectName }},
				{label: "Environment", value: func() string { return *env }},
				{label: "Action", value: func() string { return *action }},
				textReviewField(ctx, "AWS SSO profile", profile, "", func(s string) bool {
					return validInput("text", s)
				}),
				regionField, accountField, stateField,
			}
			if offeredGitHub && !reconcile {
				rows = append(rows, reviewField{label: "GitHub setup", value: func() string { return "Skip" }})
			}
			if cli.spec.Memory != 0 {
				memoryValue := fmt.Sprint(*memory)
				rows = append(rows, textReviewField(ctx, "Lambda memory (MB)", &memoryValue, "", func(s string) bool {
					n, err := strconv.Atoi(s)
					if err == nil && n >= 128 && n <= 10240 {
						*memory = n
						return true
					}
					return false
				}))
			}
			if reconcile {
				rows = append(rows, options.reviewGitHub(ctx)...)
			}
			if needsInputs {
				rows = append(rows, inputs.reviewFields(ctx, *env)...)
			}
			if e = reviewSettings(ctx, rows); e != nil {
				return e
			}
		}
	}
	if !*ci {
		inputs.showDNSGuidance(ctx)
		if *account == "auto" {
			*account = ""
		}
	}
	if !validRegion(*region) {
		return errors.New("valid region required")
	}
	if *ci && (*account == "" || *backend == "") {
		return errors.New("--account and --backend required in CI")
	}
	if *account != "" && !pattern(`^\d{12}$`, *account) {
		return errors.New("valid AWS account required")
	}
	newBucket := !*ci && canCreateBucket && *backend == "new"
	if !newBucket && !validBackend(*backend) {
		return errors.New("valid S3 state location required")
	}
	if e = verifySavedTarget(saved, *account, *region, *backend); e != nil {
		return e
	}
	if ui.browser == nil {
		who, _, e = checkConnection(ctx, root, Settings{Profile: *profile, Region: *region, Account: *account}, *ci, *workers, false)
		if e != nil {
			return e
		}
	}
	ui.phase("connect", "Verify cloud access")
	restoreConnection, e := connectionEnv(*profile, *region, *ci)
	if e != nil {
		return e
	}
	defer restoreConnection()
	*account = who.Account
	ui.heading("AWS account")
	ui.detail("Account", *account)
	ui.detail("Identity", who.Arn)
	ui.detail("Region", *region)
	ui.detail("Environment", *env)
	ui.detail("Stack", cli.spec.StackPrefix+*env)

	if !*ci {
		yes, err := ui.choose("Proceed with this AWS account? [Y/n]", "Use this AWS account", true, "Account "+*account, "Region "+*region, "Stack "+cli.spec.StackPrefix+*env)
		if err != nil {
			return err
		}
		if !yes {
			return ui.declined("account selection cancelled")
		}
	}
	if submittedCredentials != credentials {
		if err := saveCredentials(credentialPath, vaultPassphrase, submittedCredentials); err != nil {
			return fmt.Errorf("save provider credentials: %w", err)
		}
	}
	vaultPassphrase = ""
	ctx = context.WithValue(ctx, credentialsKey{}, submittedCredentials)
	if *action == "github-teardown" {
		return teardownGitHub(ctx, cli.spec, private, projectName, *env, *account)
	}
	if newBucket {
		name, err := bootstrapBucket(ctx, private, *account, *region, cli.spec.BucketPrefix)
		if err != nil {
			return err
		}
		*backend = "s3://" + name + "/infra-composer/" + projectName
	}
	ui.phase("prepare", "Verify deployment state")
	ui.heading("Deployment state")
	ui.detail("S3 location", *backend)

	if ui.browser == nil || newBucket {
		if e = verifyStateBucket(ctx, root, *backend, *account, *region); e != nil {
			return e
		}
	}
	bucket, _, _ := strings.Cut(strings.TrimPrefix(*backend, "s3://"), "/")
	// Recover a legacy bootstrap receipt only when it matches the verified target.
	if _, err := os.Stat(filepath.Join(private, "state-bucket.json")); os.IsNotExist(err) && projectName == cli.spec.PulumiProject && (*action == "setup" || *action == "preview" || *action == "deploy") {
		if prior, err := os.ReadFile(filepath.Join(legacy, "state-bucket.json")); err == nil {
			var r bucketReceipt
			if json.Unmarshal(prior, &r) == nil && r.Created && r.Name == bucket && r.Account == *account && r.Region == *region {
				if e = atomicWrite(filepath.Join(private, "state-bucket.json"), prior); e != nil {
					return e
				}
			}
		}
	}
	// Only annotate a state bucket this wizard actually bootstrapped; preserve shared bucket ownership.
	if receiptBytes, err := os.ReadFile(filepath.Join(private, "state-bucket.json")); err == nil {
		var receipt bucketReceipt
		if json.Unmarshal(receiptBytes, &receipt) != nil {
			return errors.New("invalid state bucket receipt")
		}
		if (*action == "setup" || *action == "preview" || *action == "deploy") && receipt.Created && receipt.Name == bucket && receipt.Account == *account && receipt.Region == *region {
			if e = tagManagedStateBucket(ctx, bucket, *account, projectName, *env); e != nil {
				return e
			}
		}
	}
	b, _ := json.MarshalIndent(Settings{1, *env, *profile, *region, *account, *backend}, "", "  ")
	if e = atomicWrite(path, b); e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(localEnv, "settings.json"), b); e != nil {
		return e
	}

	projectFlags.Version = 1
	projectFlags.Environment = *env
	flagsBytes, _ := json.MarshalIndent(projectFlags, "", "  ")
	if e = atomicWrite(projectFlagsPath, flagsBytes); e != nil {
		return e
	}
	if cli.spec.Memory != 0 {
		flags, _ := json.MarshalIndent(struct{ Version, Memory int }{1, *memory}, "", "  ")
		for _, dir := range []string{private, localEnv} {
			if e = atomicWrite(filepath.Join(dir, "flags.json"), flags); e != nil {
				return e
			}
		}
	}
	if needsInputs {
		if e = inputs.save(cli.spec.InputsFile, private); e != nil {
			return e
		}
	}
	if reconcile {
		ui.phase("prepare", "Prepare GitHub deployment")
		options.Settings = Settings{1, *env, *profile, *region, *account, *backend}
		options.variables = inputs.githubVariables()
		if e = options.applyGitHub(ctx); e != nil {
			return e
		}
	} else if offeredGitHub {
		if e = atomicWrite(filepath.Join(private, "github-skipped"), []byte("skipped\n")); e != nil {
			return e
		}
	}
	if *action == "github-setup" {
		return nil
	}

	ui.phase("prepare", "Prepare the Pulumi stack")
	dir := filepath.Join(root, cli.spec.PulumiDir)
	stack := cli.spec.StackPrefix + *env
	if e = command(ctx, dir, "pulumi", "login", *backend, "--non-interactive"); e != nil {
		return e
	}
	if e = cli.ensureStack(ctx, dir, stack, filepath.Join(private, "stack.json"), stackRecord{Account: *account, Region: *region, Backend: *backend}, *action == "setup" || *action == "preview" || *action == "deploy",
		func(args ...string) error { return command(ctx, dir, append([]string{"pulumi"}, args...)...) },
		func() ([]byte, error) {
			return outputIn(ctx, dir, "pulumi", "stack", "ls", "--json", "--non-interactive")
		}); e != nil {
		return e
	}

	// Restore authoritative encrypted configuration before setting nonsecret values.
	if e = restoreStackConfig(func() ([]byte, error) {
		cmd := toolCommand(ctx, "pulumi", "config", "refresh", "--force", "--stack", stack, "--non-interactive")
		cmd.Dir = dir
		return cmd.CombinedOutput()
	}); e != nil {
		return e
	}
	if cli.spec.StackIdentity {
		stateBytes, e := outputIn(ctx, dir, "pulumi", "stack", "export", "--stack", stack)
		if e != nil {
			return e
		}
		if *adoptLegacy {
			if *ci {
				return errors.New("legacy identity adoption requires interactive review")
			}
			if e = ui.deliberate("Type the stack name after verifying its original ownership", stack); e != nil {
				return e
			}
		}
		e = verifyStackIdentity(stateBytes, projectName, *env, *adoptLegacy)
		clear(stateBytes)
		if e != nil {
			return e
		}
	}
	var activeFeatureReport *featureReport
	if *action == "report" || *action == "preview" || *action == "deploy" {
		attempt := cli.newFeatureReport(ctx, root, dir, stack, private, local, Settings{1, *env, *profile, *region, *account, *backend}, projectName, *action)
		if *action == "report" {
			if err := attempt.finish(ctx, nil, *refreshReport); err != nil {
				return err
			}
			attempt.publish(ctx, false)
			return nil
		}
		eventFile, err := os.CreateTemp(private, "preview-events-*.json")
		if err != nil {
			return err
		}
		attempt.previewLog = eventFile.Name()
		eventFile.Close()
		defer func() {
			if err := attempt.finish(ctx, runErr, attempt.applied); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("feature report could not be saved: %w", err))
			}
			attempt.publish(ctx, false)
		}()
		activeFeatureReport = attempt
		attempt.publish(ctx, false)

	}
	if *action == "status" {
		raw, e := outputIn(ctx, dir, "pulumi", "stack", "output", "--stack", stack, "--json")
		if e != nil {
			return e
		}
		safe, e := safeOutputs(raw, cli.spec.Outputs)
		if e != nil {
			return e
		}
		ui.result(string(safe))
		return nil
	}
	var index *discoveryClient
	if cli.spec.Discovery != nil && (*action == "deploy" || *action == "destroy") {
		index, e = openDiscovery(ctx, private, *env, discoveryURL, discoveryRegion, *region)
		if e != nil {
			return e
		}
	}
	if *action == "destroy" {
		if *ci {
			return errors.New("interactive teardown confirmation required")
		}
		ui.heading("Destroy this stack?")
		ui.detail("Stack", stack)
		ui.note("This removes stack resources; protected resources can still block deletion.")
		if err := ui.deliberate("Type the stack name to destroy", stack); err != nil {
			return err
		}
		ui.phase("apply", "Destroy the confirmed stack")
		if index != nil {
			revision, err := index.begin(ctx, private)
			if err != nil {
				return err
			}
			if _, err := index.change(ctx, discovery.Change{Operation: "retire", Revision: revision}); err != nil {
				return fmt.Errorf("discovery retirement failed: %w", err)
			}
		}
		return command(ctx, dir, "pulumi", "destroy", "--stack", stack, "--yes")
	}

	if *action != "preview" && *action != "deploy" && *action != "setup" {
		return errors.New("unknown action")
	}
	if e = cli.spec.setMissingConfig(ctx, dir, stack); e != nil {
		return e
	}
	ui.phase("prepare", "Build Lambda")
	ui.heading("Build Lambda")
	archive, e := cli.build(ctx, root, *force)
	if e != nil {
		return e
	}
	config := [][2]string{{"aws:region", *region}, {"archive", archive}}
	if cli.spec.Memory != 0 {
		config = append(config, [2]string{"memory", fmt.Sprint(*memory)})
	}
	if cli.spec.StackIdentity {
		config = append(config, [2]string{"deploymentProject", projectName}, [2]string{"deploymentEnvironment", *env})
	}
	for _, field := range inputs {
		config = append(config, [2]string{strings.ToLower(field.spec.Name), field.value})
	}
	for _, pair := range config {
		if e = command(ctx, dir, "pulumi", "config", "set", "--stack", stack, "--non-interactive", "--", pair[0], pair[1]); e != nil {
			return e
		}
	}
	if *action == "setup" {
		return nil
	}
	plan := filepath.Join(private, "plan.json")
	defer os.Remove(plan)
	artifactUnlock, e := operationLock(filepath.Join(filepath.Dir(archive), "build.lock"))
	if e != nil {
		return e
	}
	defer artifactUnlock()
	archiveBytes, e := os.ReadFile(archive)
	if e != nil {
		return e
	}
	approvedArchive := sha256.Sum256(archiveBytes)
	ui.phase("preview", "Preview changes with Pulumi")
	ui.heading("Preview changes")
	if e = command(ctx, dir, "pulumi", "preview", "--stack", stack, "--save-plan", plan, "--event-log", activeFeatureReport.previewLog, "--non-interactive"); e != nil {
		return e
	}
	if *action == "preview" && ui.browser == nil {
		return nil
	}

	planBytes, e := os.ReadFile(plan)
	if e != nil {
		return e
	}
	if ui.browser != nil {
		activeFeatureReport.publish(ctx, true)
		if err := ui.preview(planBytes); err != nil {
			return err
		}
	}
	if *action == "preview" {
		return nil
	}
	destructive, e := destructivePlan(planBytes)
	if e != nil {
		return e
	}
	ui.heading("Previewed target")
	for _, pair := range [][2]string{{"Project", projectName}, {"Account", *account}, {"Region", *region}, {"Environment", *env}, {"Stack", stack}, {"Backend", *backend}, {"Saved plan", plan}} {
		ui.detail(pair[0], pair[1])
	}
	if destructive {
		if *ci {
			return errors.New("destructive plan requires separate operator review")
		}
		ui.heading("This plan includes deletion or replacement")
		ui.detail("Stack", stack)
		if e = ui.deliberate("Type the stack name to approve deletion/replacement", stack); e != nil {
			return e
		}
	}

	if *ci {
		if !*approve {
			return errors.New("--approve required for noninteractive deploy")
		}
	} else if !destructive {
		if e = confirm(ctx, "Account "+*account, "Region "+*region, "Stack "+stack); e != nil {
			return e
		}
	}
	if current, err := os.ReadFile(plan); err != nil || sha256.Sum256(current) != sha256.Sum256(planBytes) {
		return errors.New("saved plan changed after approval; preview again")
	}
	if current, err := os.ReadFile(archive); err != nil || sha256.Sum256(current) != approvedArchive {
		return errors.New("artifact changed after preview; preview again")
	}
	var revision int64
	if index != nil {
		revision, e = index.begin(ctx, private)
		if e != nil {
			return e
		}
	}
	activeFeatureReport.applied = true
	ui.phase("apply", "Apply the saved plan")
	ui.heading("Apply previewed changes")
	if e = command(ctx, dir, "pulumi", "up", "--stack", stack, "--plan", plan, "--yes", "--non-interactive"); e != nil {
		return e
	}
	outputs, e := outputIn(ctx, dir, "pulumi", "stack", "output", "--stack", stack, "--json")
	if e != nil {
		return e
	}
	outputs, e = safeOutputs(outputs, cli.spec.Outputs)
	if e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(private, "outputs.json"), outputs); e != nil {
		return e
	}
	success, _ := json.Marshal(struct {
		Version                                                           int
		Project, Environment, Account, Region, Backend, Stack, PlanSHA256 string
		AppliedAt                                                         time.Time
	}{1, projectName, *env, *account, *region, *backend, stack, canonicalHash(json.RawMessage(planBytes)), time.Now().UTC()})
	if e = atomicWrite(filepath.Join(private, "applied.json"), success); e != nil {
		return e
	}
	if index != nil {
		ui.heading("Publish environment discovery")
		connection, err := cli.spec.Discovery.fromOutputs(outputs)
		if err != nil {
			return fmt.Errorf("stack applied, discovery publication skipped: %w", err)
		}
		if err := cli.spec.Discovery.ready(ctx, index.http, connection); err != nil {
			return fmt.Errorf("stack applied, discovery publication skipped: %w", err)
		}
		if _, err := index.change(ctx, discovery.Change{Operation: "publish", Revision: revision, Configuration: &connection}); err != nil {
			return fmt.Errorf("stack applied, discovery publication failed: %w", err)
		}
	}
	ui.heading("Deployment complete")
	ui.result(string(outputs))
	return nil
}
func outputIn(ctx context.Context, dir string, args ...string) ([]byte, error) {
	c := toolCommand(ctx, args...)
	c.Dir = dir
	c.Stderr = os.Stderr
	capture := captureDiagnostics(ctx, c)
	result, err := c.Output()
	capture(err)
	return result, err
}
