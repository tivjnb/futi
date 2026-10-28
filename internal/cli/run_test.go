package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/execmodule"
)

func TestFormatRunErrorTemplate(t *testing.T) {
	tmplErr := &execmodule.TemplateError{
		Cmd:   "echo {{index .args_list 0}}",
		Cause: errors.New("slice index out of range"),
	}
	out := formatRunError(tmplErr)
	if !strings.Contains(out, "failed to render template") {
		t.Fatalf("expected friendly header, got: %q", out)
	}
	if !strings.Contains(out, "Template: echo {{index .args_list 0}}") {
		t.Fatalf("expected template shown, got: %q", out)
	}
	if !strings.Contains(out, "Cause: slice index out of range") {
		t.Fatalf("expected cause shown, got: %q", out)
	}
	// Длинная цепочка обёрток не должна появляться.
	if strings.Contains(out, "modulerunner:") || strings.Contains(out, "execmodule:") {
		t.Fatalf("expected no wrapper chain, got: %q", out)
	}
}

func TestFormatRunErrorGeneric(t *testing.T) {
	err := errors.New("some other error")
	if out := formatRunError(err); !strings.Contains(out, "run: some other error") {
		t.Fatalf("expected generic run: prefix, got: %q", out)
	}
}

// futi, чей ребёнок убит SIGINT, сам должен погибнуть от SIGINT (а не выйти
// с кодом 130): иначе bash не прервёт цикл по Ctrl+C.
func TestFutiDiesFromSignalWhenChildInterrupted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	tmp := t.TempDir()
	bin := builtFutiBinary(t)
	mods := filepath.Join(tmp, "mods", "m")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "name: m\ncmd: \"sh -c 'kill -INT $$'\"\n"
	if err := os.WriteFile(filepath.Join(mods, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "m")
	cmd.Env = append(os.Environ(),
		"FUTI_MODULES_DIR="+filepath.Join(tmp, "mods"),
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	err := cmd.Run()
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Fatalf("futi завершился %v, want гибель от SIGINT", cmd.ProcessState)
	}
}

// --- F70/F71: строгий порядок путь → флаги → `-- args` ---

func TestRunParsedPassesStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat is not available on Windows")
	}
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "m.yaml"), "cmd: cat\n")
	rt := testRuntime(t, core.Options{ModulesDir: dir})
	var stdout, stderr bytes.Buffer
	code, err := runParsed(t.Context(), strings.NewReader("x"), &stdout, &stderr, rt, &parsedArgs{command: "m"})
	if code != 0 || err != nil || stdout.String() != "x" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q, want 0, nil, %q", code, err, stdout.String(), stderr.String(), "x")
	}
}
