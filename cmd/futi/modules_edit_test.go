package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"futi/internal/discovery"
	"futi/internal/execmodule"
)

// writeManifestFixture создаёт пакет <dir>/<name>/config.yaml с комментарием
// и существующими полями (GlobalSource резолвит name → name/config.yaml).
// Возвращает путь к манифесту.
func writeManifestFixture(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name, "config.yaml")
	body := `# module comment
name: qr
description: QR codes
attach: "true" # keep me
commands:
  make:
    description: Make a QR code
    cmd: "echo make"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAddCommandPreservesCommentsAndFields(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")

	if err := addCommandToManifest(path, "scan", "echo scan", "Scan a QR code"); err != nil {
		t.Fatalf("addCommandToManifest: %v", err)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{"# module comment", "name: qr", "description: QR codes",
		"attach: \"true\"", "# keep me", "scan", "echo scan"} {
		if !strings.Contains(text, want) {
			t.Fatalf("result missing %q:\n%s", want, text)
		}
	}

	// Результат — валидный манифест, команда доступна.
	mod, err := execmodule.LoadModule(path, filepath.Dir(path))
	if err != nil {
		t.Fatalf("LoadModule after edit: %v", err)
	}
	if _, ok := mod.Commands["scan"]; !ok {
		t.Fatal("command scan not found after edit")
	}
	if mod.Attach != "true" {
		t.Fatalf("attach must survive edit, got %q", mod.Attach)
	}
}

func TestAddCommandDuplicateError(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")
	err := addCommandToManifest(path, "make", "echo dup", "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRemoveCommandKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")

	if err := removeCommandFromManifest(path, "make"); err != nil {
		t.Fatalf("removeCommandFromManifest: %v", err)
	}

	mod, err := execmodule.LoadModule(path, filepath.Dir(path))
	if err != nil {
		t.Fatalf("LoadModule after edit: %v", err)
	}
	if _, ok := mod.Commands["make"]; ok {
		t.Fatal("command make must be removed")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# module comment") {
		t.Fatalf("comments must survive removal:\n%s", string(data))
	}
}

func TestRemoveCommandMissingError(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")
	err := removeCommandFromManifest(path, "nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestRunModulesAddCommandCLI(t *testing.T) {
	dir := t.TempDir()
	writeManifestFixture(t, dir, "qr")
	src := discovery.NewGlobalSource(dir)

	var b bytes.Buffer
	err := runModulesAddCommand(&b, src, []string{"qr", "scan", "--desc", "Scan QR"})
	if err != nil {
		t.Fatalf("runModulesAddCommand: %v", err)
	}
	if !strings.Contains(b.String(), `Added command "scan"`) {
		t.Fatalf("unexpected output: %s", b.String())
	}

	mod, err := execmodule.LoadModule(filepath.Join(dir, "qr", "config.yaml"), filepath.Join(dir, "qr"))
	if err != nil {
		t.Fatalf("LoadModule after edit: %v", err)
	}
	if _, ok := mod.Commands["scan"]; !ok {
		t.Fatal("command scan not found")
	}
	if mod.Commands["scan"].Description != "Scan QR" {
		t.Fatalf("unexpected description: %q", mod.Commands["scan"].Description)
	}
}

func TestRunModulesAddCommandDefaultCmd(t *testing.T) {
	dir := t.TempDir()
	writeManifestFixture(t, dir, "qr")
	src := discovery.NewGlobalSource(dir)

	if err := runModulesAddCommand(&bytes.Buffer{}, src, []string{"qr", "scan"}); err != nil {
		t.Fatalf("runModulesAddCommand: %v", err)
	}
	mod, err := execmodule.LoadModule(filepath.Join(dir, "qr", "config.yaml"), filepath.Join(dir, "qr"))
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	if got := mod.Commands["scan"].Cmd; got != "echo scan" {
		t.Fatalf("default cmd must be \"echo scan\", got %q", got)
	}
}
