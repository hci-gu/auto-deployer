package clone

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func UniquePath(root, repoFullName string, prNumber int, sha string) (string, error) {
	org, repo, err := parseRepoFullName(repoFullName)
	if err != nil {
		return "", err
	}
	shortSHA := sha
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	baseDir := filepath.Join(root, org, repo)
	candidate := filepath.Join(baseDir, fmt.Sprintf("pr-%d-%s", prNumber, shortSHA))
	if _, err := os.Stat(candidate); err == nil {
		suffix := time.Now().UTC().Format("20060102-150405")
		candidate = candidate + "-" + suffix
	}

	if err := os.MkdirAll(filepath.Dir(candidate), 0o755); err != nil {
		return "", fmt.Errorf("create clone root: %w", err)
	}
	return candidate, nil
}

func WithToken(cloneURL, token string) (string, error) {
	cloneURL = strings.TrimSpace(cloneURL)
	if cloneURL == "" {
		return "", fmt.Errorf("clone url is empty")
	}
	if token == "" {
		return cloneURL, nil
	}

	parsed, err := url.Parse(cloneURL)
	if err != nil {
		return "", fmt.Errorf("parse clone url: %w", err)
	}
	if parsed.Scheme != "https" {
		return cloneURL, nil
	}

	parsed.User = url.UserPassword("x-access-token", token)
	return parsed.String(), nil
}

func CloneRepo(ctx context.Context, cloneURL, token, dest, sha string) error {
	if cloneURL == "" {
		return fmt.Errorf("clone url is empty")
	}
	if dest == "" {
		return fmt.Errorf("destination is empty")
	}
	if sha == "" {
		return fmt.Errorf("sha is empty")
	}

	if err := runGit(ctx, token, "clone", cloneURL, dest); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	if err := runGit(ctx, token, "-C", dest, "checkout", sha); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}
	return nil
}

func runGit(ctx context.Context, token string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		trimmed = redactToken(trimmed, token)
		if trimmed != "" {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		return err
	}
	return nil
}

func parseRepoFullName(fullName string) (string, string, error) {
	parts := strings.SplitN(fullName, "/", 3)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo full name: %s", fullName)
	}
	org := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	if !validSegment(org) || !validSegment(repo) {
		return "", "", fmt.Errorf("invalid repo full name: %s", fullName)
	}
	return org, repo, nil
}

func validSegment(value string) bool {
	if value == "" {
		return false
	}
	if strings.Contains(value, "..") {
		return false
	}
	if strings.ContainsAny(value, "/\\") {
		return false
	}
	return true
}

func redactToken(text, token string) string {
	if token == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "REDACTED")
}
