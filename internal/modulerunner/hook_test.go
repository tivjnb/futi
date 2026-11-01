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

	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
)

func hookText(s string) execmodule.CmdSpec { return execmodule.CmdSpec{Text: s} }

func hookList(items ...string) execmodule.CmdSpec {
	return execmodule.CmdSpec{List: items, IsList: true}
}

// shHook is the explicit form of a compound hook.
func shHook(script string) execmodule.CmdSpec { return hookList("sh", "-c", script) }

func TestRunHookRunsInModuleRoot(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := RunHook(context.Background(), hookText("pwd"), dir, "m", nil, &out, &out, executor.GroupIsolateWithoutTTY, true); err != nil {
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
		hookText(`echo {{.module_name}} {{.module_root}}`), dir, "qr", nil, &out, &out, executor.GroupIsolateWithoutTTY, true)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	want := "qr " + dir
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunHookEnvVars(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := RunHook(context.Background(),
		shHook(`echo $FUTI_MODULE_NAME $FUTI_MODULE_ROOT`), dir, "qr", nil, &out, &out, executor.GroupIsolateWithoutTTY, true)
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
	err := RunHook(context.Background(), hookText(`echo {{.nope}}`), dir, "m", nil, io.Discard, io.Discard, executor.GroupIsolateWithoutTTY, true)
	if err == nil {
		t.Fatal("expected template error for unknown variable")
	}
}

func TestRunHookPropagatesExitCode(t *testing.T) {
	dir := t.TempDir()
	err := RunHook(context.Background(), shHook("exit 7"), dir, "m", nil, io.Discard, io.Discard, executor.GroupIsolateWithoutTTY, true)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code: got %d, want 7", exitErr.ExitCode())
	}
}

func TestRunHookReadsExplicitStdin(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := RunHook(context.Background(), shHook(`cat > "$FUTI_MODULE_ROOT/out"`), dir, "m", strings.NewReader("data"), &out, &out, executor.GroupIsolateWithoutTTY, true); err != nil {
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

func TestRunHookRelativeModuleRootIsAbsolute(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.MkdirAll(filepath.Join("rel", "mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := RunHook(context.Background(),
		shHook(`echo $FUTI_MODULE_ROOT {{.module_root}}`), filepath.Join("rel", "mod"), "m", nil, &out, &out, executor.GroupIsolateWithoutTTY, true)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	abs := filepath.Join(cwd, "rel", "mod")
	want := abs + " " + abs
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A string hook is a template split into argv, not a shell command line:
// under sh -c, "echo a;b" would run b as a second command.
func TestRunHookNoShell(t *testing.T) {
	var out bytes.Buffer
	if err := RunHook(context.Background(), hookText("echo a;b"), t.TempDir(), "m", nil, &out, &out, executor.GroupIsolateWithoutTTY, true); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if got, want := out.String(), "a;b\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunHookList(t *testing.T) {
	var out bytes.Buffer
	err := RunHook(context.Background(), hookList("echo", "a  b", "{{.module_name}}", ""), t.TempDir(), "mod", nil, &out, &out, executor.GroupIsolateWithoutTTY, true)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if got, want := out.String(), "a  b mod \n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunHookEnvTemplate(t *testing.T) {
	t.Setenv("FUTI_TEST_HOOK_VAR", "from env")
	var out bytes.Buffer
	err := RunHook(context.Background(), hookList("echo", "{{.env.FUTI_TEST_HOOK_VAR}}"), t.TempDir(), "m", nil, &out, &out, executor.GroupIsolateWithoutTTY, true)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if got, want := out.String(), "from env\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunHookInputRefFails(t *testing.T) {
	err := RunHook(context.Background(), hookText("echo {{.x}}"), t.TempDir(), "m", nil, io.Discard, io.Discard, executor.GroupIsolateWithoutTTY, true)
	if err == nil {
		t.Fatal("expected error for an input reference in a hook")
	}
}
