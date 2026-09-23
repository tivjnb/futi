package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateModuleName(t *testing.T) {
	valid := []string{"qr", "images/qr", "my-mod", "my_mod", "ns.sub/name-1"}
	invalid := []string{"", "..", "../etc", "a/../b", ".hidden", "/abs", "a//b", "-lead", "a/", "./a"}

	for _, name := range valid {
		if err := validateModuleName(name); err != nil {
			t.Errorf("validateModuleName(%q) unexpected error: %v", name, err)
		}
	}
	for _, name := range invalid {
		if err := validateModuleName(name); err == nil {
			t.Errorf("validateModuleName(%q) must fail", name)
		}
	}
}

func TestEnsureWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "qr")
	os.MkdirAll(inside, 0o755)
	outside := t.TempDir()

	if err := ensureWithinRoot(inside, []string{root}); err != nil {
		t.Fatalf("inside root must pass: %v", err)
	}
	if err := ensureWithinRoot(outside, []string{root}); err == nil {
		t.Fatal("outside root must fail")
	}
}

func TestEnsureWithinRootSymlinkInside(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	os.MkdirAll(root, 0o755)
	module := filepath.Join(root, "qr")
	os.MkdirAll(module, 0o755)
	// Симлинк на модуль из-за пределов root.
	link := filepath.Join(base, "link")
	os.Symlink(module, link)

	if err := ensureWithinRoot(link, []string{root}); err != nil {
		t.Fatalf("symlink into root must pass: %v", err)
	}
}

func TestConfirmDelete(t *testing.T) {
	tests := []struct {
		input string
		name  string
		want  bool
	}{
		{"y\n", "y", true},
		{"yes\n", "yes", true},
		{"Y\n", "Y", true},
		{"n\n", "n", false},
		{"\n", "empty", false},
		{"nope\n", "nope", false},
	}
	for _, tt := range tests {
		var b strings.Builder
		got, err := confirmDelete(strings.NewReader(tt.input), &b, "/some/path", 3, 4096)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %v, want %v", tt.name, got, tt.want)
		}
		if !strings.Contains(b.String(), "About to delete: /some/path") {
			t.Fatalf("%s: missing summary: %s", tt.name, b.String())
		}
	}
}
