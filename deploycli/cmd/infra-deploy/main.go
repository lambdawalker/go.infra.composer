// infra-deploy orchestrates a standalone feature deployment.
package main

import (
	"archive/zip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
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
	"strings"
	"time"
)

type Settings struct {
	Version                                        int
	Environment, Profile, Region, Account, Backend string
}

func main() {
	defer func() {
		if r := recover(); r != nil {
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
	return c.Run()
}
func output(ctx context.Context, args ...string) ([]byte, error) {
	c := toolCommand(ctx, args...)
	c.Stderr = os.Stderr
	return c.Output()
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
func vault(path string) (string, error) {
	b, e := os.ReadFile(path)
	fresh := os.IsNotExist(e)
	if e != nil && !fresh {
		return "", e
	}
	pass, e := secret("Local vault unlock passphrase:")
	if e != nil || pass == "" {
		return "", errors.New("vault unlock cancelled")
	}
	if fresh {
		again, e := secret("Confirm vault passphrase:")
		if e != nil || again != pass {
			return "", errors.New("passphrases differ")
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
	key, e := pbkdf2.Key(sha256.New, pass, b[4:20], 600000, 32)
	if e != nil {
		return "", e
	}
	defer clear(key)
	block, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
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
	pulumiPass, e := secret("Pulumi passphrase (use the existing passphrase for an existing stack):")
	if e != nil || pulumiPass == "" {
		return "", errors.New("Pulumi passphrase required")
	}
	again, e := secret("Confirm Pulumi passphrase:")
	if e != nil || again != pulumiPass {
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
func confirm() error {
	heading("Deploy the previewed changes? [Y/n]")
	yes, err := timedChoice(true)
	if err != nil {
		return err
	}
	if !yes {
		return errors.New("deployment cancelled")
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
			note("Build: reusing verified Lambda archive")
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
	if e = c.Run(); e != nil {
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
func (cli deployCLI) run() (runErr error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fs := flag.NewFlagSet("infra-deploy", flag.ContinueOnError)
	env := fs.String("environment", "", "isolated environment")
	action := fs.String("action", "", "setup, build, clean-cache, preview, deploy, status, destroy, github-setup, report (default: deploy interactively, preview in CI)")
	profile := fs.String("profile", "", "AWS SSO profile (omit in CI)")
	region := fs.String("region", "", "AWS region")
	account := fs.String("account", "", "expected AWS account")
	backend := fs.String("backend", "", "s3://bucket/dedicated-prefix")
	ci := fs.Bool("non-interactive", false, "complete explicit inputs required")
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
	if e := fs.Parse(os.Args[1:]); e != nil {
		return e
	}
	if os.Getenv("CI") != "" {
		*ci = true
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
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
	allowedActions := map[string]bool{"setup": true, "build": true, "clean-cache": true, "preview": true, "deploy": true, "status": true, "destroy": true, "github-setup": true, "report": true}
	if !allowedActions[*action] {
		return errors.New("unknown action")
	}
	banner(cli.spec, *action, *env)
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
	projectName, local, central, e := cli.projectPaths(root, *project, *ci)
	if e != nil {
		return e
	}
	// Pulumi writes shared workspace files even across environments.
	workspaceUnlock, e := operationLock(filepath.Join(local, "workspace.lock"))
	if e != nil {
		return e
	}
	defer workspaceUnlock()
	detail("Project", projectName)
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
		*env = prompt("Environment ("+strings.Join(names, ", ")+", or custom test name)", "dev")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`).MatchString(*env) || *env == "prod" || *env == "production" {
		return errors.New("use a validated non-production test environment")
	}
	if _, e = os.Stat(filepath.Join(root, "go.mod")); e != nil {
		return errors.New("run from repository root or use deploy launcher")
	}
	if *action == "build" {
		_, e := cli.build(ctx, root, *force)
		return e
	}
	projectFlags.Version = 1
	projectFlags.Environment = *env
	flagsBytes, _ := json.MarshalIndent(projectFlags, "", "  ")
	if e = atomicWrite(projectFlagsPath, flagsBytes); e != nil {
		return e
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
		deploymentFlags.Version = 1
		deploymentFlags.Memory = *memory
		fb, _ := json.MarshalIndent(deploymentFlags, "", "  ")
		if e = atomicWrite(deploymentFlagsPath, fb); e != nil {
			return e
		}
		if e = atomicWrite(filepath.Join(localEnv, "flags.json"), fb); e != nil {
			return e
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
			note("Imported previous deployment settings")
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
	if *importPath != "" {
		imported, err := importSettings(*importPath, projectName, *env)
		if err != nil {
			return err
		}
		if err = verifySavedTarget(saved, imported.Account, imported.Region, imported.Backend); err != nil {
			return err
		}
		saved = imported
		note("Using compatible imported nonsecret settings; remote discovery remains required")
	}
	if e = verifySavedTarget(saved, *account, *region, *backend); e != nil {
		return e
	}

	choose := func(v *string, prior, label, def string) error {
		if *v != "" {
			return nil
		}
		if !*ci && prior != "" && !*reconfigure {
			*v = prior
			note(label + ": using saved setting")
			return nil
		}
		if *ci {
			return fmt.Errorf("%s required in CI", label)
		}
		if prior != "" {
			def = prior
		}
		*v = prompt(label, def)
		return nil
	}
	if !*ci {
		profileDefault := ""
		profiles, err := output(ctx, "aws", "configure", "list-profiles")
		if err != nil {
			return fmt.Errorf("list AWS profiles; install AWS CLI v2 and run aws configure sso: %w", err)
		}
		for _, p := range strings.Fields(string(profiles)) {
			if profileDefault == "" || p == "default" {
				profileDefault = p
			}
		}
		if profileDefault == "" {
			return errors.New("no AWS profiles; run aws configure sso first")
		}
		if e = choose(profile, saved.Profile, "AWS SSO profile", profileDefault); e != nil {
			return e
		}
		os.Setenv("AWS_PROFILE", *profile)
	}
	for _, v := range []struct {
		p               *string
		old, label, def string
	}{{region, saved.Region, "region", "us-east-2"}} {
		if e = choose(v.p, v.old, v.label, v.def); e != nil {
			return e
		}
	}
	if !regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`).MatchString(*region) {
		return errors.New("valid region required")
	}
	if *ci && (*account == "" || *backend == "") {
		return errors.New("--account and --backend required in CI")
	}
	if *account == "" {
		*account = saved.Account
	}
	if *backend == "" {
		*backend = saved.Backend
	}

	if e = verifySavedTarget(saved, *account, *region, *backend); e != nil {
		return e
	}
	if !*ci {
		if e = verifySSOProfile(ctx, *profile); e != nil {
			return e
		}
	}
	os.Setenv("AWS_REGION", *region)
	os.Setenv("AWS_DEFAULT_REGION", *region)
	for _, tool := range []string{"aws", "pulumi", "go"} {
		if _, e = exec.LookPath(tool); e != nil {
			return fmt.Errorf("install %s and add it to PATH", tool)
		}
	}
	if e = prerequisites(ctx, *workers, []func() error{
		func() error {
			b, e := output(ctx, "aws", "--version")
			if e != nil || !strings.Contains(string(b), "aws-cli/2.") {
				return errors.New("AWS CLI v2 required")
			}
			return nil
		},
		func() error {
			b, e := output(ctx, "pulumi", "version")
			if e != nil || !strings.HasPrefix(strings.TrimSpace(string(b)), "v3.") {
				return errors.New("Pulumi CLI v3 required")
			}
			return nil
		},
	}); e != nil {
		return e
	}
	identity, e := authenticatedIdentity(*ci, *profile, func() ([]byte, error) { return output(ctx, "aws", "sts", "get-caller-identity", "--output", "json") }, func() error { return command(ctx, root, "aws", "sso", "login", "--profile", *profile) })

	if e != nil {
		return e
	}
	var who struct{ Account, Arn string }
	if json.Unmarshal(identity, &who) != nil || !regexp.MustCompile(`^\d{12}$`).MatchString(who.Account) {
		return errors.New("invalid AWS identity")
	}
	if *account != "" && *account != who.Account {
		return errors.New("AWS account differs from saved/explicit target; use --reconfigure to deliberately change it")
	}
	if *ci && !strings.Contains(who.Arn, ":assumed-role/") {
		return errors.New("CI requires an assumed role")
	}
	if !*ci && !strings.Contains(who.Arn, ":assumed-role/AWSReservedSSO_") {
		return errors.New("interactive deployments require an IAM Identity Center SSO profile")
	}
	*account = who.Account
	heading("AWS account")
	detail("Account", *account)
	detail("Identity", who.Arn)
	detail("Region", *region)
	detail("Environment", *env)
	detail("Stack", cli.spec.StackPrefix+*env)

	if !*ci {
		heading("Proceed with this AWS account? [Y/n]")
		yes, err := timedChoice(true)
		if err != nil {
			return err
		}
		if !yes {
			return errors.New("account selection cancelled")
		}
	}
	if *backend == "" {
		if *ci {
			return errors.New("--backend required in CI")
		}
		heading("Do you have an S3 bucket for deployment state? [y/N]")
		note("No: create a private, encrypted, versioned bucket automatically.")
		existing, err := timedChoice(false)
		if err != nil {
			return err
		}
		if existing {
			name := prompt("S3 bucket name", "")
			*backend = "s3://" + name + "/infra-composer/" + projectName
		} else {
			if *action == "status" || *action == "destroy" || *action == "report" || *action == "github-setup" {
				return errors.New("select an existing state bucket for status/destroy")
			}
			name, err := bootstrapBucket(ctx, private, *account, *region, cli.spec.BucketPrefix)
			if err != nil {
				return err
			}
			*backend = "s3://" + name + "/infra-composer/" + projectName
		}
	}
	if !regexp.MustCompile(`^s3://[a-z0-9.-]+/[^?\s]+$`).MatchString(*backend) {
		return errors.New("valid S3 state location required")
	}
	heading("Deployment state")
	detail("S3 location", *backend)

	// Verify the existing state bucket before using it. Bootstrap is deliberately separate.
	bucket := strings.Split(strings.TrimPrefix(*backend, "s3://"), "/")[0]
	if e = command(ctx, root, "aws", "s3api", "head-bucket", "--bucket", bucket, "--expected-bucket-owner", *account); e != nil {
		return e
	}
	for _, check := range []struct{ cmd, field, want string }{{"get-bucket-versioning", "Status", "Enabled"}, {"get-bucket-encryption", "ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm", ""}, {"get-public-access-block", "PublicAccessBlockConfiguration", ""}, {"get-bucket-location", "LocationConstraint", ""}} {
		b, e := output(ctx, "aws", "s3api", check.cmd, "--bucket", bucket, "--expected-bucket-owner", *account, "--query", check.field, "--output", "json")
		if e != nil {
			return e
		}
		if check.cmd == "get-bucket-versioning" && strings.TrimSpace(string(b)) != `"Enabled"` {
			return errors.New("state bucket must have versioning enabled")
		}
		if check.cmd == "get-bucket-encryption" && !strings.Contains(string(b), "AES256") && !strings.Contains(string(b), "aws:kms") {
			return errors.New("state bucket encryption unavailable")
		}
		if check.cmd == "get-public-access-block" {
			var fields map[string]bool
			if json.Unmarshal(b, &fields) != nil || !fields["BlockPublicAcls"] || !fields["BlockPublicPolicy"] || !fields["IgnorePublicAcls"] || !fields["RestrictPublicBuckets"] {
				return errors.New("state bucket must block all public access")
			}
		}
		if check.cmd == "get-bucket-location" {
			loc := strings.Trim(strings.TrimSpace(string(b)), `"`)
			if loc == "null" {
				loc = "us-east-1"
			}
			if loc != *region {
				return errors.New("state bucket region differs from selected region")
			}
		}
	}
	policyBytes, e := output(ctx, "aws", "s3api", "get-bucket-policy", "--bucket", bucket, "--expected-bucket-owner", *account, "--output", "json")
	if e != nil {
		return e
	}
	if e = requireStateTLS(policyBytes, bucket); e != nil {
		return e
	}
	statePrefix := strings.SplitN(strings.TrimPrefix(*backend, "s3://"), "/", 2)[1]
	if _, e = output(ctx, "aws", "s3api", "list-objects-v2", "--bucket", bucket, "--prefix", statePrefix+"/", "--max-keys", "1", "--expected-bucket-owner", *account, "--output", "json"); e != nil {
		return fmt.Errorf("state prefix is inaccessible: %w", e)
	}
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

	if os.Getenv("PULUMI_CONFIG_PASSPHRASE") == "" {
		if *ci {
			return errors.New("PULUMI_CONFIG_PASSPHRASE secret required")
		}
		vaultPath := filepath.Join(localEnv, "vault.bin")
		legacyVault := ""
		if projectName == cli.spec.PulumiProject {
			legacyVault = filepath.Join(legacy, "vault.bin")
		}
		pass, e := loadProjectVault(vaultPath, legacyVault)
		if e != nil {
			return e
		}
		os.Setenv("PULUMI_CONFIG_PASSPHRASE", pass)
		defer os.Unsetenv("PULUMI_CONFIG_PASSPHRASE")
	}
	githubOpts := githubOptions{target: cli.spec, Root: root, Settings: Settings{1, *env, *profile, *region, *account, *backend}, Role: *githubRole, Repository: *githubRepo, Environment: *githubEnv, Project: projectName, Policy: *githubPolicy, SecretVersion: *githubSecretVersion, Force: *forceReconcile, Private: private}
	if *action == "github-setup" {
		if *ci {
			return errors.New("GitHub setup requires interactive review")
		}
		if e = inputs.load(cli.spec.InputsFile, private, *ci, *reconfigure); e != nil {
			return e
		}
		githubOpts.variables = inputs.githubVariables()
		return githubWizard(ctx, githubOpts, true)
	}
	if cli.spec.GitHub.Prompt && !*ci && !*skipGitHub && (*action == "setup" || *action == "deploy") {
		if e = githubWizard(ctx, githubOpts, *reconfigure); e != nil {
			return e
		}
	}

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
		cmd := exec.CommandContext(ctx, "pulumi", "config", "refresh", "--force", "--stack", stack, "--non-interactive")
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
			if e = deliberate("Type the stack name after verifying its original ownership", stack); e != nil {
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
			return attempt.finish(ctx, nil, *refreshReport)
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
		}()
		activeFeatureReport = attempt

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
		fmt.Println(string(safe))
		return nil
	}
	if *action == "destroy" {
		if *ci {
			return errors.New("interactive teardown confirmation required")
		}
		heading("Destroy this stack?")
		detail("Stack", stack)
		note("This removes stack resources; protected resources can still block deletion.")
		if err := deliberate("Type the stack name to destroy", stack); err != nil {
			return err
		}
		return command(ctx, dir, "pulumi", "destroy", "--stack", stack, "--yes")
	}

	if *action != "preview" && *action != "deploy" && *action != "setup" {
		return errors.New("unknown action")
	}
	if e = inputs.load(cli.spec.InputsFile, private, *ci, *reconfigure); e != nil {
		return e
	}
	if e = cli.spec.setMissingConfig(ctx, dir, stack); e != nil {
		return e
	}
	heading("Build Lambda")
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
		config = append(config, [2]string{strings.ToLower(field.spec.Name), *field.value})
	}
	for _, pair := range config {
		if e = command(ctx, dir, "pulumi", "config", "set", pair[0], pair[1], "--stack", stack, "--non-interactive"); e != nil {
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
	heading("Preview changes")
	if e = command(ctx, dir, "pulumi", "preview", "--stack", stack, "--save-plan", plan, "--event-log", activeFeatureReport.previewLog, "--non-interactive"); e != nil {
		return e
	}
	if *action == "preview" {
		return nil
	}

	planBytes, e := os.ReadFile(plan)
	if e != nil {
		return e
	}
	destructive, e := destructivePlan(planBytes)
	if e != nil {
		return e
	}
	heading("Previewed target")
	for _, pair := range [][2]string{{"Project", projectName}, {"Account", *account}, {"Region", *region}, {"Environment", *env}, {"Stack", stack}, {"Backend", *backend}, {"Saved plan", plan}} {
		detail(pair[0], pair[1])
	}
	if destructive {
		if *ci {
			return errors.New("destructive plan requires separate operator review")
		}
		heading("This plan includes deletion or replacement")
		detail("Stack", stack)
		if e = deliberate("Type the stack name to approve deletion/replacement", stack); e != nil {
			return e
		}
	}

	if *ci {
		if !*approve {
			return errors.New("--approve required for noninteractive deploy")
		}
	} else if !destructive {
		if e = confirm(); e != nil {
			return e
		}
	}
	if current, err := os.ReadFile(plan); err != nil || sha256.Sum256(current) != sha256.Sum256(planBytes) {
		return errors.New("saved plan changed after approval; preview again")
	}
	if current, err := os.ReadFile(archive); err != nil || sha256.Sum256(current) != approvedArchive {
		return errors.New("artifact changed after preview; preview again")
	}
	activeFeatureReport.applied = true
	heading("Apply previewed changes")
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
	heading("Deployment complete")
	fmt.Println(string(outputs))
	return nil
}
func outputIn(ctx context.Context, dir string, args ...string) ([]byte, error) {
	c := toolCommand(ctx, args...)
	c.Dir = dir
	c.Stderr = os.Stderr
	return c.Output()
}
