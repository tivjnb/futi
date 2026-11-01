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

// A relative program path is resolved from the module root, not from the cwd
// of futi: a same-named script in the caller's cwd must not run.
func TestRunHookRelativeProgramFromModuleRoot(t *testing.T) {
	module, foreign := t.TempDir(), t.TempDir()
	script := func(dir, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "setup.sh"), []byte("#!/bin/sh\necho "+text+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script(module, "module")
	script(foreign, "foreign")
	if err := os.MkdirAll(filepath.Join(module, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(module, "setup.sh"), filepath.Join(module, "scripts", "x.sh")); err != nil {
		t.Fatal(err)
	}
	script(module, "module")
	t.Chdir(foreign)
	for _, prog := range []string{"./setup.sh", "scripts/x.sh"} {
		var out bytes.Buffer
		if err := RunHook(context.Background(), hookList(prog), module, "m", nil, &out, &out, executor.GroupIsolateWithoutTTY, true); err != nil {
			t.Fatalf("RunHook(%s): %v", prog, err)
		}
		if got := strings.TrimSpace(out.String()); got != "module" {
			t.Fatalf("RunHook(%s) ran %q, want the module script", prog, got)
		}
	}
}

func TestRunHookEmptyArgv(t *testing.T) {
	t.Setenv("FUTI_TEST_EMPTY", "")
	err := RunHook(context.Background(), hookText("{{.env.FUTI_TEST_EMPTY}}"), t.TempDir(), "m", nil, io.Discard, io.Discard, executor.GroupIsolateWithoutTTY, true)
	if err == nil || err.Error() != "modulerunner: hook rendered to empty argv" {
		t.Fatalf("err = %v, want hook rendered to empty argv", err)
	}
}

// TestHookProgramPath: a program path with a separator that is relative to the
// module root is joined with it; drive-relative, root-relative and absolute
// paths and bare names are left as they are. win selects the Windows rules,
// so the table runs on any OS.
func TestHookProgramPath(t *testing.T) {
	root := filepath.Join("ROOT", "mod")
	tests := []struct {
		prog   string
		win    bool
		joined bool
	}{
		{"scripts/x.sh", false, true},
		{"./setup.sh", false, true},
		{"/abs/x.sh", false, false},
		{"x.sh", false, false},
		{`a\b`, false, false}, // a backslash is an ordinary character
		{"scripts/x.sh", true, true},
		{`scripts\x.exe`, true, true},
		{`.\x.exe`, true, true},
		{"x.exe", true, false},
		{`C:\bin\x.exe`, true, false},
		{`C:/bin/x.exe`, true, false},
		{`C:foo\x.exe`, true, false}, // relative to the cwd of drive C
		{`c:x.exe`, true, false},
		{`\foo\x.exe`, true, false}, // relative to the root of the current drive
		{`/foo/x.exe`, true, false},
		{`\\server\share\x.exe`, true, false},
	}
	for _, tt := range tests {
		got := hookProgramPath(tt.prog, root, tt.win)
		if tt.joined {
			if want := filepath.Join(root, tt.prog); got != want {
				t.Errorf("hookProgramPath(%q, win=%v) = %q, want %q", tt.prog, tt.win, got, want)
			}
		} else if got != tt.prog {
			t.Errorf("hookProgramPath(%q, win=%v) = %q, want it unchanged", tt.prog, tt.win, got)
		}
	}
}

// A relative module root and a relative program path: the script is found
// from the absolute module root, not from the cwd.
func TestRunHookRelativeRootAndProgram(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	rel := filepath.Join("rel", "mod")
	if err := os.MkdirAll(filepath.Join(rel, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rel, "scripts", "x.sh"), []byte("#!/bin/sh\npwd\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunHook(context.Background(), hookList("scripts/x.sh"), rel, "m", nil, &out, &out, executor.GroupIsolateWithoutTTY, true); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(cwd, rel))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("hook ran in %q, want %q", got, want)
	}
}
