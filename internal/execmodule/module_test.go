package execmodule

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModuleParsesCommands(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "docker.yaml")
	writeFile(t, manifestPath, `
name: docker
description: Docker management
commands:
  ps:
    description: List containers
    cmd: "docker ps"
  build:
    description: Build image
    params:
      - name: tag
        type: string
        required: true
      - name: file
        type: string
        default: Dockerfile
    cmd: "docker build -t {{.tag}} -f {{.file}} ."
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Name != "docker" {
		t.Fatalf("unexpected name: %q", mod.Name)
	}
	if mod.Description != "Docker management" {
		t.Fatalf("unexpected description: %q", mod.Description)
	}
	if mod.Root != dir {
		t.Fatalf("unexpected root: %q", mod.Root)
	}
	if len(mod.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(mod.Commands))
	}

	ps, ok := mod.Commands["ps"]
	if !ok {
		t.Fatal("command 'ps' not found")
	}
	if ps.Cmd != "docker ps" {
		t.Fatalf("unexpected ps cmd: %q", ps.Cmd)
	}
	if ps.Description != "List containers" {
		t.Fatalf("unexpected ps description: %q", ps.Description)
	}

	build, ok := mod.Commands["build"]
	if !ok {
		t.Fatal("command 'build' not found")
	}
	if len(build.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(build.Params))
	}
	if build.Params[0].Name != "tag" || !build.Params[0].Required {
		t.Fatalf("unexpected param 0: %+v", build.Params[0])
	}
	if build.Params[1].Name != "file" || build.Params[1].Default != "Dockerfile" {
		t.Fatalf("unexpected param 1: %+v", build.Params[1])
	}
}

func TestLoadModuleMissingCommand(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "empty.yaml")
	writeFile(t, manifestPath, `name: empty`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := mod.Commands["nonexistent"]; ok {
		t.Fatal("expected command not found, but it exists")
	}
}

func TestLoadModuleInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bad.yaml")
	writeFile(t, manifestPath, `name: bad\n  :: invalid`)

	_, err := LoadModule(manifestPath, dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}
