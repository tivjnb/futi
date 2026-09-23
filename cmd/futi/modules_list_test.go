package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"futi/internal/metayaml"
)

func TestScanInstalledFindsModulesWithMeta(t *testing.T) {
	root := t.TempDir()
	// Установленный модуль (пакет с meta).
	installFixtureModule(t, root, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/qr"}})
	// Установленный модуль в namespace.
	installFixtureModule(t, filepath.Join(root, "images"), "resize", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/resize"}})
	// Ручной модуль БЕЗ meta — не должен попасть.
	installFixtureModule(t, root, "handmade", nil)
	// Посторонний каталог с excluded-именем — не должен попасть.
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)

	got := scanInstalled([]string{root})
	if len(got) != 2 {
		t.Fatalf("expected 2 installed modules, got %d: %+v", len(got), got)
	}
	names := []string{got[0].Name, got[1].Name}
	if names[0] != "images.resize" || names[1] != "qr" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestDotName(t *testing.T) {
	if got := dotName(filepath.Join("images", "qr")); got != "images.qr" {
		t.Fatalf("dotName: got %q", got)
	}
}

func TestRunModulesListOutput(t *testing.T) {
	root := t.TempDir()
	installFixtureModule(t, root, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/qr"}})
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir()) // без ./.futi

	var b bytes.Buffer
	if err := runModulesList(&b, nil); err != nil {
		t.Fatalf("runModulesList: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "qr") || !strings.Contains(out, "/src/qr") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestRunModulesListEmpty(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())
	var b bytes.Buffer
	if err := runModulesList(&b, nil); err != nil {
		t.Fatalf("runModulesList: %v", err)
	}
	if !strings.Contains(b.String(), "No installed modules found") {
		t.Fatalf("unexpected output:\n%s", b.String())
	}
}
