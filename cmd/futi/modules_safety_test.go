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

func TestValidateResolveName(t *testing.T) {
	for _, name := range []string{"qr", "dev.deploy", "my-mod"} {
		if err := validateResolveName(name); err != nil {
			t.Errorf("validateResolveName(%q) unexpected error: %v", name, err)
		}
	}
	for _, name := range []string{"", "dev/config", "images/qr", "a/../b", "/abs"} {
		if err := validateResolveName(name); err == nil {
			t.Errorf("validateResolveName(%q) must fail", name)
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

func TestEnsureWithinRoot_RejectsRoot(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "m")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}

	if err := ensureWithinRoot(root, []string{root}); err == nil {
		t.Error("сам корень должен отвергаться")
	}
	if err := ensureWithinRoot(link, []string{root}); err == nil {
		t.Error("симлинк на корень должен отвергаться")
	}
	if err := ensureWithinRoot(sub, []string{root}); err != nil {
		t.Errorf("root/sub должен приниматься: %v", err)
	}
}

func TestCleanupEmptyParents_RootNotRemoved(t *testing.T) {
	variants := map[string]func(t *testing.T, tmp string) string{
		"trailing slash": func(t *testing.T, tmp string) string { return filepath.Join(tmp, "m2") + "/" },
		"dot":            func(t *testing.T, tmp string) string { return filepath.Join(tmp, "m2") + "/." },
		"dotdot":         func(t *testing.T, tmp string) string { return filepath.Join(tmp, "x", "..", "m2") },
		"symlink": func(t *testing.T, tmp string) string {
			link := filepath.Join(tmp, "link")
			if err := os.Symlink(filepath.Join(tmp, "m2"), link); err != nil {
				t.Fatal(err)
			}
			return link
		},
	}
	for name, mkRoot := range variants {
		t.Run(name, func(t *testing.T) {
			tmp := t.TempDir()
			ns := filepath.Join(tmp, "m2", "ns")
			for _, d := range []string{filepath.Join(tmp, "x"), ns} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			cleanupEmptyParents(ns, []string{mkRoot(t, tmp)})

			if _, err := os.Stat(ns); !os.IsNotExist(err) {
				t.Error("пустой ns должен быть удалён")
			}
			if _, err := os.Stat(filepath.Join(tmp, "m2")); err != nil {
				t.Errorf("корень m2 должен остаться: %v", err)
			}
		})
	}
}

// Корень лежит строго внутри цели (source /x, глобальный /x/ns/modules, цель
// /x/ns): удаление цели снесло бы корень.
func TestEnsureWithinRootRejectsTargetContainingRoot(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "x")
	ns := filepath.Join(src, "ns")
	global := filepath.Join(ns, "modules")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureWithinRoot(ns, []string{src, global}); err == nil {
		t.Error("цель, содержащая корень, должна отвергаться")
	}
}
