package executor

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/testutil"
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
	// Именно nil, а не context.TODO(): проверяется отказ на nil-контексте.
	//nolint:staticcheck // SA1012
	if err := RunWithConfig(nil, &Config{}, "script.sh"); err == nil {
		t.Fatal("expected an error for nil context")
	}
}

func TestRunWithConfigNilStdinIsEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat недоступен на Windows")
	}
	testutil.ReplaceStdin(t, "inherited")

	var buf bytes.Buffer
	if err := RunWithConfig(context.Background(), &Config{Stdout: &buf}, "cat"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Fatalf("stdout = %q, want empty (nil Stdin must not inherit os.Stdin)", got)
	}
}

func TestRunWithConfigNilStdoutDiscards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh недоступен на Windows")
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	runErr := RunWithConfig(context.Background(), &Config{}, "sh", "-c", "echo out; echo err >&2")
	os.Stdout, os.Stderr = origOut, origErr
	outW.Close()
	errW.Close()
	if runErr != nil {
		t.Fatalf("RunWithConfig: %v", runErr)
	}
	for name, r := range map[string]*os.File{"stdout": outR, "stderr": errR} {
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 0 {
			t.Fatalf("%s leaked %q, want nothing", name, data)
		}
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
