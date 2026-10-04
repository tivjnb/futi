package executor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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

// replaceStdin подменяет os.Stdin файлом с content до конца теста.
func replaceStdin(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		f.Close()
	})
}

func TestRunWithConfigInheritsStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat недоступен на Windows")
	}
	replaceStdin(t, "hello")

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
	replaceStdin(t, "hello")

	var buf bytes.Buffer
	cfg := &Config{Stdin: strings.NewReader("x"), Stdout: &buf}
	if err := RunWithConfig(context.Background(), cfg, "cat"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if got := buf.String(); got != "x" {
		t.Fatalf("stdout = %q, want %q", got, "x")
	}
}

// waitForFile ждёт появления файла (опрос с дедлайном).
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("файл %s не появился за 5с", path)
}

// signalChildEnv возвращает окружение с путями маркеров MARK и READY.
func signalChildEnv(t *testing.T) (env []string, mark, ready string) {
	t.Helper()
	dir := t.TempDir()
	mark = filepath.Join(dir, "mark")
	ready = filepath.Join(dir, "ready")
	env = append(os.Environ(), "MARK="+mark, "READY="+ready)
	return env, mark, ready
}

func TestRunWithConfigForwardsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	env, mark, ready := signalChildEnv(t)
	script := `trap 'echo term > "$MARK"; exit 0' TERM; echo ready > "$READY"; while :; do sleep 0.05; done`

	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(context.Background(), &Config{Env: env}, "sh", "-c", script)
	}()
	waitForFile(t, ready)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithConfig: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	data, err := os.ReadFile(mark)
	if err != nil {
		t.Fatalf("ребёнок не получил SIGTERM: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "term" {
		t.Fatalf("mark = %q, want %q", got, "term")
	}
}

func TestRunWithConfigDoesNotForwardSIGINT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	env, mark, ready := signalChildEnv(t)
	// SIGINT шлём только тестовому процессу (не группе): ребёнок его не получает.
	script := `trap 'echo int > "$MARK"' INT; echo ready > "$READY"; sleep 0.3; exit 0`

	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(context.Background(), &Config{Env: env}, "sh", "-c", script)
	}()
	waitForFile(t, ready)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithConfig: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("SIGINT переслан ребёнку, а не должен")
	}
}

func TestRunWithConfigCancelSendsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	env, mark, ready := signalChildEnv(t)
	script := `trap 'echo term > "$MARK"; exit 0' TERM; echo ready > "$READY"; while :; do sleep 0.05; done`

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(ctx, &Config{Env: env}, "sh", "-c", script)
	}()
	waitForFile(t, ready)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	data, err := os.ReadFile(mark)
	if err != nil {
		t.Fatalf("ребёнок не получил SIGTERM при отмене: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "term" {
		t.Fatalf("mark = %q, want %q", got, "term")
	}
}

func TestRunWithConfigWrapsExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh недоступен на Windows")
	}
	err := RunWithConfig(context.Background(), &Config{}, "sh", "-c", "exit 3")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("err = %v, want *exec.ExitError с кодом 3", err)
	}
}

// Сигнал, проигнорированный при старте futi (nohup, фоновая команда шелла),
// должен остаться игнорируемым у ребёнка: Notify сбросил бы SIG_IGN при exec.
func TestRunWithConfigKeepsInheritedIgnoredSignal(t *testing.T) {
	if _, err := os.Stat("/proc/self/status"); err != nil {
		t.Skip("нужен /proc")
	}
	signal.Ignore(syscall.SIGHUP)
	t.Cleanup(func() { signal.Reset(syscall.SIGHUP) })

	var buf bytes.Buffer
	if err := RunWithConfig(context.Background(), &Config{Stdout: &buf}, "grep", "SigIgn", "/proc/self/status"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	fields := strings.Fields(buf.String())
	if len(fields) != 2 {
		t.Fatalf("неожиданный вывод: %q", buf.String())
	}
	mask, err := strconv.ParseUint(fields[1], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	// SIGHUP = 1 -> бит 0.
	if mask&1 == 0 {
		t.Fatalf("SIGHUP не игнорируется у ребёнка: SigIgn = %s", fields[1])
	}
}
