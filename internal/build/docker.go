package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Config struct {
	Dockerfile string
	Platform   string
	UseBuildx  bool
}

func BuildAndPush(ctx context.Context, repoURL string, sha string, imageRef string, cfg Config) error {
	if repoURL == "" {
		return fmt.Errorf("repo URL is empty")
	}
	if sha == "" {
		return fmt.Errorf("sha is empty")
	}
	if imageRef == "" {
		return fmt.Errorf("image ref is empty")
	}
	if cfg.Dockerfile == "" {
		cfg.Dockerfile = "Dockerfile"
	}
	if cfg.Platform == "" {
		cfg.Platform = "linux/amd64"
	}

	dir, err := os.MkdirTemp("", "preview-build-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	if err := run(ctx, dir, "git", "init"); err != nil {
		return err
	}
	if err := run(ctx, dir, "git", "remote", "add", "origin", repoURL); err != nil {
		return err
	}
	if err := run(ctx, dir, "git", "fetch", "--depth", "1", "origin", sha); err != nil {
		return err
	}
	if err := run(ctx, dir, "git", "checkout", "FETCH_HEAD"); err != nil {
		return err
	}

	dockerfilePath := filepath.Join(dir, cfg.Dockerfile)
	if _, err := os.Stat(dockerfilePath); err != nil {
		return fmt.Errorf("dockerfile not found at %s", dockerfilePath)
	}

	if cfg.UseBuildx {
		if err := run(ctx, dir, "docker", "buildx", "build", "--push", "--tag", imageRef, "--platform", cfg.Platform, "-f", dockerfilePath, "."); err != nil {
			return err
		}
		if err := run(ctx, dir, "docker", "image", "rm", "-f", imageRef); err != nil {
			return err
		}
	} else {
		if err := run(ctx, dir, "docker", "build", "-f", dockerfilePath, "-t", imageRef, "."); err != nil {
			return err
		}
		if err := run(ctx, dir, "docker", "push", imageRef); err != nil {
			return err
		}
		if err := run(ctx, dir, "docker", "image", "rm", "-f", imageRef); err != nil {
			return err
		}
	}

	return nil
}

func run(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}
