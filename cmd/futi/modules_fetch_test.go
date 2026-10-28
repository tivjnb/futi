package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/executor"
)

// makeTarGz собирает .tar.gz из map rel→content. Возвращает путь.
func makeTarGz(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	path := filepath.Join(dir, "fixture.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// makeZip собирает .zip из map rel→content.
func makeZip(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, "fixture.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	return path
}

func TestExtractArchiveTarGzStripsWrapper(t *testing.T) {
	dir := t.TempDir()
	archive := makeTarGz(t, dir, map[string]string{
		"mod-1.0/config.yaml":  "name: qr\n",
		"mod-1.0/scripts/r.sh": "echo hi\n",
	})

	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "mod-1.0", "config.yaml")); err != nil {
		t.Fatalf("expected extracted wrapper dir: %v", err)
	}

	// resolveModuleDir находит вложенный модуль.
	moduleDir, err := resolveModuleDir(dest)
	if err != nil {
		t.Fatalf("resolveModuleDir: %v", err)
	}
	if filepath.Base(moduleDir) != "mod-1.0" {
		t.Fatalf("unexpected module dir: %s", moduleDir)
	}
}

func TestExtractArchiveZip(t *testing.T) {
	dir := t.TempDir()
	archive := makeZip(t, dir, map[string]string{"config.yaml": "name: z\n"})

	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "config.yaml")); err != nil || string(data) != "name: z\n" {
		t.Fatalf("unexpected extraction result: %q, %v", string(data), err)
	}
}

func TestExtractArchiveTarGzZipSlipRejected(t *testing.T) {
	dir := t.TempDir()
	// Валидный tar.gz с атакующим entry "../evil.txt".
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "../evil.txt", Mode: 0o644, Size: 5}
	tw.WriteHeader(hdr)
	tw.Write([]byte("evil\n"))
	tw.Close()
	gz.Close()
	archive := filepath.Join(dir, "evil.tar.gz")
	os.WriteFile(archive, buf.Bytes(), 0o644)

	dest := t.TempDir()
	err := extractArchive(archive, dest)
	if err == nil {
		t.Fatal("expected zip-slip rejection")
	}
	if _, statErr := os.Stat(filepath.Join(dest, "..", "evil.txt")); statErr == nil {
		t.Fatal("evil file must not be created")
	}
}

func TestExtractArchiveUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.tar.bz2")
	os.WriteFile(path, []byte("x"), 0o644)
	if err := extractArchive(path, dir); err == nil {
		t.Fatal("expected unsupported-format error")
	}
}

func TestSafeJoinEdgeCases(t *testing.T) {
	dest := t.TempDir()

	// Легитимные имена — принимаются.
	ok := []string{"config.yaml", "./file.txt", "dir/", "mod-1.0/config.yaml", "file..txt", "a/b/c.txt"}
	for _, name := range ok {
		target, err := safeJoin(dest, name)
		if err != nil {
			t.Errorf("safeJoin(%q) unexpected error: %v", name, err)
			continue
		}
		rel, relErr := filepath.Rel(dest, target)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("safeJoin(%q) = %q escapes dest", name, target)
		}
	}

	// Traversal — отвергаются.
	bad := []string{"../evil.txt", "..", "a/../../b", "a/../.."}
	for _, name := range bad {
		if _, err := safeJoin(dest, name); err == nil {
			t.Errorf("safeJoin(%q) must be rejected", name)
		}
	}
}

// gitRunTest выполняет git в dir с тестовыми author/committer.
func gitRunTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cfg := &executor.Config{
		Dir: dir,
		Env: append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com"),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	if err := executor.RunWithConfig(context.Background(), cfg, "git", args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func TestFetchGitLocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "config.yaml"),
		[]byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunTest(t, repo, "init")
	gitRunTest(t, repo, "add", ".")
	gitRunTest(t, repo, "commit", "-m", "init")

	tempBase := t.TempDir()
	dir, commit, err := fetchGit(context.Background(), executor.Config{}, repo, "", tempBase)
	if err != nil {
		t.Fatalf("fetchGit: %v", err)
	}
	if len(commit) != 40 { // full SHA
		t.Fatalf("expected 40-char commit SHA, got %q", commit)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("module not cloned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must be removed after clone")
	}
}
