package executor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"futi/internal/testutil"
)

func TestRunWithConfigExecutesScriptWithArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts are not supported in this test on Windows")
	}

	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "script.sh")

	script := []byte("#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$2\"\n")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	var stdout bytes.Buffer
	if err := RunWithConfig(context.Background(), &Config{Stdout: &stdout}, scriptPath, "alpha", "beta"); err != nil {
		t.Fatalf("run script: %v", err)
	}

	if got := stdout.String(); got != "alpha beta\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestConfigBuilders(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := NewConfig().
		WithDir("/tmp").
		WithEnv([]string{"A=B"}).
		WithStdout(&stdout).
		WithStderr(&stderr)

	if cfg.Dir != "/tmp" {
		t.Fatalf("unexpected dir: %q", cfg.Dir)
	}
	if len(cfg.Env) != 1 || cfg.Env[0] != "A=B" {
		t.Fatalf("unexpected env: %#v", cfg.Env)
	}
	if cfg.Stdout != &stdout {
		t.Fatal("unexpected stdout writer")
	}
	if cfg.Stderr != &stderr {
		t.Fatal("unexpected stderr writer")
	}
}

func TestRunWithConfigRejectsNilContext(t *testing.T) {
	if err := RunWithConfig(context.TODO(), &Config{}, "script.sh"); err == nil {
		t.Fatal("expected an error for nil context")
	}
}

func TestRunWithConfigInheritsStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat недоступен на Windows")
	}
	testutil.ReplaceStdin(t, "hello")

	var buf bytes.Buffer
	if err := RunWithConfig(context.Background(), &Config{Stdout: &buf}, "cat"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if got := buf.String(); got != "hello" {
		t.Fatalf("stdout = %q, want %q", got, "hello")
	}
}

func TestRunWithConfigExplicitStdinWins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat недоступен на Windows")
	}
	testutil.ReplaceStdin(t, "hello")

	var buf bytes.Buffer
	cfg := &Config{Stdin: strings.NewReader("x"), Stdout: &buf}
	if err := RunWithConfig(context.Background(), cfg, "cat"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if got := buf.String(); got != "x" {
		t.Fatalf("stdout = %q, want %q", got, "x")
	}
}
