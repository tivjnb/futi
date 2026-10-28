package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tivjnb/futi/internal/metayaml"
)

// installFixtureModule создаёт в dir пакет модуля name с config.yaml и
// (опционально) __futi_meta.yaml. Возвращает корень source.
func installFixtureModule(t *testing.T, dir, name string, meta *metayaml.Meta) string {
	t.Helper()
	moduleDir := filepath.Join(dir, name)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\ncommands:\n  run:\n    cmd: echo run\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		if err := metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), meta); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunModulesPath(t *testing.T) {
	root := installFixtureModule(t, t.TempDir(), "qr", nil)
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	got, err := newTestManager(t, io.Discard, nil).Path("qr")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(root, "qr"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
