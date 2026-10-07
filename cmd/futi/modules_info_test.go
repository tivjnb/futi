package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tivjnb/futi/internal/discovery"
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
	var b bytes.Buffer
	if err := runModulesPath(&b, discovery.NewGlobalSource(root), []string{"qr"}); err != nil {
		t.Fatalf("runModulesPath: %v", err)
	}
	want := filepath.Join(root, "qr")
	if got := strings.TrimSpace(b.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunModulesInfoWithMeta(t *testing.T) {
	root := t.TempDir()
	meta := &metayaml.Meta{
		Lockfile: metayaml.Lockfile{
			Source: "/src/qr", Type: "local", Hash: "sha256:ab",
			InstalledAt: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
		},
		About: &metayaml.About{Author: "foo@example.com", License: "MIT"},
	}
	installFixtureModule(t, root, "qr", meta)

	var b bytes.Buffer
	if err := runModulesInfo(&b, discovery.NewGlobalSource(root), []string{"qr"}); err != nil {
		t.Fatalf("runModulesInfo: %v", err)
	}
	out := b.String()
	for _, want := range []string{"Path:", "Source: /src/qr", "Type: local", "Hash: sha256:ab",
		"Installed at: 2026-09-21 10:00:00", "Author: foo@example.com", "License: MIT"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunModulesInfoWithoutMeta(t *testing.T) {
	root := installFixtureModule(t, t.TempDir(), "qr", nil)
	var b bytes.Buffer
	if err := runModulesInfo(&b, discovery.NewGlobalSource(root), []string{"qr"}); err != nil {
		t.Fatalf("runModulesInfo: %v", err)
	}
	if !strings.Contains(b.String(), "Installed: no") {
		t.Fatalf("expected 'Installed: no' note:\n%s", b.String())
	}
}

func TestRunModulesPathUsageError(t *testing.T) {
	if err := runModulesPath(nil, discovery.NewGlobalSource(t.TempDir()), nil); err == nil {
		t.Fatal("expected usage error")
	}
}
