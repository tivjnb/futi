package modules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tivjnb/futi/internal/metayaml"
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

	got := scanInstalled([]moduleRoot{{Dir: root}})
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

// Каталог *.futi-backup — не установленный модуль.
func TestScanInstalledSkipsBackupDirs(t *testing.T) {
	root := t.TempDir()
	meta := &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/qr"}}
	installFixtureModule(t, root, "qr.futi-backup", meta)
	installFixtureModule(t, root, "other", meta)

	got := scanInstalled([]moduleRoot{{Dir: root}})
	if len(got) != 1 || got[0].Name != "other" {
		t.Fatalf("want only other, got %+v", got)
	}
}
