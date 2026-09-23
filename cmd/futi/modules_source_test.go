package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectSourceType(t *testing.T) {
	existing := t.TempDir()

	tests := []struct {
		name                   string
		src                    string
		fromLocal, fromGit, ar bool
		want                   sourceKind
		wantErr                bool
	}{
		{"existing path is local", existing, false, false, false, kindLocal, false},
		{"git url", "https://github.com/foo/bar.git", false, false, false, kindGit, false},
		{"git host without .git", "https://github.com/foo/bar", false, false, false, kindGit, false},
		{"archive url", "https://example.com/mod.tar.gz", false, false, false, kindArchive, false},
		{"zip url", "https://example.com/mod.zip", false, false, false, kindArchive, false},
		{"flag local overrides", "https://github.com/foo/bar", true, false, false, kindLocal, false},
		{"flag git overrides", existing, false, true, false, kindGit, false},
		{"two flags error", "x", true, true, false, 0, true},
		{"unknown", "definitely-not-a-path-or-url", false, false, false, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detectSourceType(tt.src, tt.fromLocal, tt.fromGit, tt.ar)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFallbackName(t *testing.T) {
	tests := []struct{ src, want string }{
		{"/path/to/qr", "qr"},
		{"https://github.com/foo/bar.git", "bar"},
		{"https://example.com/mod.tar.gz", "mod"},
		{"https://example.com/mod.zip", "mod"},
		{"/path/to/dir/", "dir"},
	}
	for _, tt := range tests {
		if got := fallbackName(tt.src); got != tt.want {
			t.Errorf("fallbackName(%q) = %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestCopyTreeFilteredSkipsExcluded(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, ".git", "objects"), 0o755)
	os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(src, "config.yaml"), []byte("name: a\n"), 0o644)
	os.MkdirAll(filepath.Join(src, "scripts"), 0o755)
	os.WriteFile(filepath.Join(src, "scripts", "run.sh"), []byte("echo\n"), 0o644)

	dst := t.TempDir()
	if err := copyTreeFiltered(src, dst); err != nil {
		t.Fatalf("copyTreeFiltered: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must be skipped")
	}
	if _, err := os.Stat(filepath.Join(dst, "scripts", "run.sh")); err != nil {
		t.Fatalf("scripts/run.sh must be copied: %v", err)
	}
}

func TestFetchLocalSourceDir(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "config.yaml"), []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644)

	tempBase := t.TempDir()
	dir, err := fetchLocalSource(src, tempBase)
	if err != nil {
		t.Fatalf("fetchLocalSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("config.yaml not copied: %v", err)
	}
}

func TestFetchLocalSourceSingleYaml(t *testing.T) {
	src := t.TempDir()
	yamlPath := filepath.Join(src, "qr.yaml")
	os.WriteFile(yamlPath, []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644)

	tempBase := t.TempDir()
	dir, err := fetchLocalSource(yamlPath, tempBase)
	if err != nil {
		t.Fatalf("fetchLocalSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("single yaml must be wrapped into package: %v", err)
	}
}

func TestFetchLocalSourceNotAModule(t *testing.T) {
	src := t.TempDir() // пустой каталог
	tempBase := t.TempDir()
	_, err := fetchLocalSource(src, tempBase)
	if err == nil {
		t.Fatal("expected error for dir without config.yaml")
	}
}

func TestResolveModuleDirStripsWrapper(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "mod-1.2.3")
	os.MkdirAll(inner, 0o755)
	os.WriteFile(filepath.Join(inner, "config.yaml"), []byte("name: x\n"), 0o644)

	got, err := resolveModuleDir(root)
	if err != nil {
		t.Fatalf("resolveModuleDir: %v", err)
	}
	if got != inner {
		t.Fatalf("got %q, want %q", got, inner)
	}
}

func TestFetchSourceLocalDispatch(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "config.yaml"), []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644)

	tempBase := t.TempDir()
	dir, commit, err := fetchSource(context.Background(), src, kindLocal, "", tempBase)
	if err != nil {
		t.Fatalf("fetchSource: %v", err)
	}
	if commit != "" {
		t.Fatalf("local source must have empty commit, got %q", commit)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("module not fetched: %v", err)
	}
}
