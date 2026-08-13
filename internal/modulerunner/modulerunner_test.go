package modulerunner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"futi/internal/discovery"
)

func TestRunExecutesCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "greet.sh")
	script := []byte("#!/bin/sh\nprintf 'hello %s\\n' \"$1\"\n")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
commands:
  greet:
    params:
      - name: name
        type: string
        required: true
    cmd: "./greet.sh {{.name}}"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "testmod", "greet", map[string]string{"name": "world"}, &stdout, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "hello world\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "hello world\n")
	}
}

func TestRunModuleNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "nonexistent", "cmd", map[string]string{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}

func TestRunCommandNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
commands:
  exist:
    cmd: "echo hi"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "testmod", "nonexistent", map[string]string{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "nonexistent") || !strings.Contains(err.Error(), "testmod") {
		t.Fatalf("error should mention command and module names, got: %v", err)
	}
}

func TestRunMissingRequiredParamReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
commands:
  build:
    params:
      - name: tag
        type: string
        required: true
    cmd: "echo {{.tag}}"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "testmod", "build", map[string]string{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
}

func TestListCommandsReturnsCommandsWithDescriptions(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
description: Test module
commands:
  greet:
    description: Greet someone
    cmd: "echo hi"
  build:
    description: Build artifact
    cmd: "echo build"
  plain:
    cmd: "echo plain"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)
	cmds, err := ListCommands(source, "testmod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(cmds))
	}

	byName := make(map[string]string)
	for _, c := range cmds {
		byName[c.Name] = c.Description
	}
	if byName["greet"] != "Greet someone" {
		t.Fatalf("unexpected greet description: %q", byName["greet"])
	}
	if byName["build"] != "Build artifact" {
		t.Fatalf("unexpected build description: %q", byName["build"])
	}
	if byName["plain"] != "" {
		t.Fatalf("expected empty description for plain, got %q", byName["plain"])
	}
}

func TestListCommandsModuleNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	source := discovery.NewGlobalSource(dir)
	_, err := ListCommands(source, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}
