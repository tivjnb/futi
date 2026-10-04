package modulerunner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHookRunsInModuleRoot(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := RunHook(context.Background(), "pwd", dir, "m", &out, &out); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	// pwd может вернуть резолвнутый путь — сравниваем через EvalSymlinks.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != real {
		t.Fatalf("hook cwd: got %q, want %q", got, real)
	}
}

func TestRunHookTemplateVars(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := RunHook(context.Background(),
		`echo {{.module_name}} {{quote .module_root}}`, dir, "qr", &out, &out)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	// sh -c: shell поглощает кавычки от {{quote ...}} — echo получает
	// /tmp/... как один argv-токен и печатает его без кавычек.
	want := "qr " + dir
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunHookEnvVars(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := RunHook(context.Background(),
		`echo $FUTI_MODULE_NAME $FUTI_MODULE_ROOT`, dir, "qr", &out, &out)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	want := "qr " + dir
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunHookUnknownVarFails(t *testing.T) {
	dir := t.TempDir()
	err := RunHook(context.Background(), `echo {{.nope}}`, dir, "m", io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected template error for unknown variable")
	}
}

func TestRunHookPropagatesExitCode(t *testing.T) {
	dir := t.TempDir()
	err := RunHook(context.Background(), "exit 7", dir, "m", io.Discard, io.Discard)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code: got %d, want 7", exitErr.ExitCode())
	}
}

func TestRunHookInheritsStdin(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	if err := os.WriteFile(in, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		f.Close()
	})

	var out bytes.Buffer
	if err := RunHook(context.Background(), `cat > "$FUTI_MODULE_ROOT/out"`, dir, "m", &out, &out); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("out = %q, want %q", got, "data")
	}
}
