package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tivjnb/futi/internal/executor"
)

// fetchGit клонирует репозиторий (--depth=1) в tempBase/repo, удаляет .git
// и возвращает каталог модуля. commit — full SHA HEAD (до удаления .git).
func fetchGit(ctx context.Context, url, ref, tempBase string) (string, string, error) {
	dest := filepath.Join(tempBase, "repo")

	args := []string{"clone", "--depth=1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dest)
	if err := executor.RunWithConfig(ctx, executor.NewConfig(), "git", args...); err != nil {
		return "", "", fmt.Errorf("git clone: %w", err)
	}

	// full SHA до удаления .git.
	var buf bytes.Buffer
	cfg := executor.NewConfig().WithDir(dest).WithStdout(&buf)
	if err := executor.RunWithConfig(ctx, cfg, "git", "rev-parse", "HEAD"); err != nil {
		return "", "", fmt.Errorf("git rev-parse: %w", err)
	}
	commit := buf.String()
	if i := strings.IndexByte(commit, '\n'); i >= 0 {
		commit = commit[:i]
	}

	if err := cleanupExcludedDirs(dest); err != nil {
		return "", "", err
	}
	dir, err := resolveModuleDir(dest)
	if err != nil {
		return "", "", err
	}
	return dir, commit, nil
}

// fetchArchive скачивает архив (curl -fsSL) и распаковывает в
// tempBase/extracted. Возвращает каталог модуля.
func fetchArchive(ctx context.Context, url, tempBase string) (string, error) {
	if _, err := exec.LookPath("curl"); err != nil {
		return "", fmt.Errorf("curl not found; install curl to install from web archives")
	}

	archivePath := filepath.Join(tempBase, "archive"+archiveExt(url))
	f, err := os.Create(archivePath)
	if err != nil {
		return "", err
	}
	cfg := executor.NewConfig().WithStdout(f)
	err = executor.RunWithConfig(ctx, cfg, "curl", "-fsSL", url)
	f.Close()
	if err != nil {
		return "", fmt.Errorf("download archive: %w", err)
	}

	extracted := filepath.Join(tempBase, "extracted")
	if err := os.MkdirAll(extracted, 0o755); err != nil {
		return "", err
	}
	if err := extractArchive(archivePath, extracted); err != nil {
		return "", err
	}
	return resolveModuleDir(extracted)
}
