package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func readTeardownCommand(ctx context.Context, args ...string) ([]byte, error) {
	cmd := toolCommand(ctx, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// teardownGitHub removes only the recorded protected deployment environment.
// A role created by this wizard is removed when ownership and trust match.
func teardownGitHub(ctx context.Context, spec manifest, private, project, environment, account string) error {
	ui := wizard(ctx)
	settings, err := readGitHubSettings(private)
	if err != nil {
		return err
	}
	if settings.Repository != spec.Repository || settings.Environment != spec.GitHub.EnvironmentPrefix+environment {
		return errors.New("no matching saved GitHub deployment configuration for this environment")
	}
	roleARN := settings.Role
	if roleARN == "" && spec.GitHub.ManagedPolicy != "" {
		roleARN = "arn:aws:iam::" + account + ":role/" + project + "-" + environment + "-deploy"
	}
	receiptPath := filepath.Join(private, "github-"+settings.Environment+".json")
	var receipt githubReceipt
	raw, err := os.ReadFile(receiptPath)
	if err != nil || json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 ||
		receipt.Repository != settings.Repository || receipt.Environment != settings.Environment ||
		receipt.Role != roleARN || receipt.CompletedAt.IsZero() {
		return errors.New("completed GitHub deployment receipt required before removing its configuration")
	}
	if err := ui.deliberate("Type the GitHub environment to remove its secrets, variables and protection rules", settings.Environment); err != nil {
		return err
	}
	if !credentialsFrom(ctx).configured(githubField) {
		if _, err := output(ctx, "gh", "auth", "status", "--hostname", "github.com"); err != nil {
			if err := ui.login(settings.Root, "GitHub", "gh", "auth", "login", "--hostname", "github.com", "--web"); err != nil {
				return err
			}
		}
	}
	base := "repos/" + settings.Repository + "/environments/" + url.PathEscape(settings.Environment)
	present := true
	if raw, err := readTeardownCommand(ctx, "gh", "api", base); err != nil {
		if !strings.Contains(err.Error(), "HTTP 404") {
			return fmt.Errorf("inspect GitHub environment: %w", err)
		}
		present = false
	} else if _, err := decodeObject(raw); err != nil {
		return err
	}
	ui.phase("apply", "Remove GitHub deployment configuration")
	if present {
		if err := command(ctx, settings.Root, "gh", "api", "--method", "DELETE", base); err != nil {
			return err
		}
	}
	if spec.GitHub.ManagedPolicy != "" && roleARN != "" {
		name, ok := strings.CutPrefix(roleARN, "arn:aws:iam::"+account+":role/")
		if !ok || strings.Contains(name, "/") {
			return errors.New("saved GitHub role differs from the selected AWS account")
		}
		roleBytes, err := readTeardownCommand(ctx, "aws", "iam", "get-role", "--role-name", name, "--output", "json")
		if err != nil {
			if !strings.Contains(err.Error(), "NoSuchEntity") {
				return fmt.Errorf("inspect GitHub deployment role: %w", err)
			}
			roleBytes = nil
		}
		var role struct {
			Role struct {
				Arn string
			}
		}
		if len(roleBytes) != 0 && (json.Unmarshal(roleBytes, &role) != nil || role.Role.Arn != roleARN) {
			return errors.New("GitHub deployment role identity changed")
		}
		tags := make(map[string]string)
		if len(roleBytes) != 0 {
			tagBytes, err := readTeardownCommand(ctx, "aws", "iam", "list-role-tags", "--role-name", name, "--output", "json")
			if err != nil {
				return fmt.Errorf("inspect GitHub role ownership: %w", err)
			}
			var inventory struct {
				Tags []struct{ Key, Value string }
			}
			if json.Unmarshal(tagBytes, &inventory) != nil {
				return errors.New("invalid GitHub role tags")
			}
			for _, tag := range inventory.Tags {
				tags[tag.Key] = tag.Value
			}
		}
		if len(roleBytes) != 0 && tags["ic:managed-by"] == "infra-composer" &&
			tags["ic:owner"] == "github.com/"+settings.Repository &&
			tags["Project"] == project && tags["Environment"] == environment {
			// Delete only the inline policy declared by this manifest.
			if _, err := readTeardownCommand(ctx, "aws", "iam", "delete-role-policy", "--role-name", name, "--policy-name", spec.GitHub.ManagedPolicy); err != nil && !strings.Contains(err.Error(), "NoSuchEntity") {
				return err
			}
			if err := command(ctx, settings.Root, "aws", "iam", "delete-role", "--role-name", name); err != nil {
				return err
			}
		} else {
			ui.note("OIDC role retained because it is not marked as managed by this project")
		}
	}
	for _, path := range []string{receiptPath, filepath.Join(private, "github-settings.json"), filepath.Join(private, "github-setup-plan.json")} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	ui.note("GitHub environment removed. The shared OIDC provider remains available to other projects.")
	return nil
}
