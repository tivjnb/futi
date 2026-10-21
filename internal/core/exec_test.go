package core_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

func TestResolveTarget(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "m.yaml")
	writeFile(t, manifest, "name: mod\ncommands:\n  c:\n    cmd: echo hi\n")
	t.Chdir(dir)
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	tgt, err := rt.Resolve("m", "c")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := tgt.Command().Cmd; got != "echo hi" {
		t.Errorf("Command().Cmd = %q, want %q", got, "echo hi")
	}
	if got := tgt.ModuleName(); got != "mod" {
		t.Errorf("ModuleName() = %q, want %q", got, "mod")
	}
	if got := tgt.Manifest(); got != manifest {
		t.Errorf("Manifest() = %q, want %q", got, manifest)
	}
}

func TestResolveErrorClasses(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "nodef.yaml"), "name: nodef\ncommands:\n  c:\n    cmd: echo hi\n")
	writeFile(t, filepath.Join(dir, "group.yaml"), "name: group\ncommands:\n  g:\n    commands:\n      leaf:\n        cmd: echo hi\n")
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	src, err := rt.Source()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, ns, cmd string
		want          error
	}{
		{"missing module", "nope", "c", core.ErrNotFound},
		{"missing command", "nodef", "zzz", core.ErrNotFound},
		{"no default command", "nodef", "", core.ErrInvalidInput},
		{"invalid namespace", "a..b", "c", core.ErrInvalidInput},
		{"group requires subcommand", "group", "g", core.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rt.Resolve(tc.ns, tc.cmd)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Resolve(%q, %q) err = %v, want class %v", tc.ns, tc.cmd, err, tc.want)
			}
			_, base := modulerunner.Resolve(src, nil, tc.ns, tc.cmd)
			if base == nil || err.Error() != base.Error() {
				t.Errorf("text = %q, want %v", err, base)
			}
		})
	}
}

// newExecRT writes a module "m" with the given command and returns a Runtime.
func newExecRT(t *testing.T, opts core.Options, cmd string) (*core.Runtime, *core.Target) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "m.yaml"), "name: m\ncommands:\n  c:\n    cmd: "+cmd+"\n")
	opts.ModulesDir = dir
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := rt.Resolve("m", "c")
	if err != nil {
		t.Fatal(err)
	}
	return rt, tgt
}

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh / POSIX signals")
	}
}

func TestExecuteOutputLimit(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, `"sh -c 'printf 0123456789; printf abcdef >&2; exit 4'"`)
	var buf bytes.Buffer
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Stdout: &buf, Stderr: &buf, OutputLimit: 8})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.Len() != 8 {
		t.Errorf("len(output) = %d, want 8", buf.Len())
	}
	if !res.Truncated || res.ExitCode != 4 || res.WaitErr == nil {
		t.Errorf("res = %+v, want Truncated, ExitCode 4, WaitErr != nil", res)
	}
}

func TestExecuteExitCodeAndWaitErr(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, `"sh -c 'exit 3'"`)
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
	if res.WaitErr == nil || res.WaitErr.Error() != `executor: run "sh": exit status 3` {
		t.Errorf("WaitErr = %v, want executor: run \"sh\": exit status 3", res.WaitErr)
	}
}

func TestExecuteWorkDir(t *testing.T) {
	skipWindows(t)
	run := func(t *testing.T, cmd, workDir string) string {
		t.Helper()
		rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, cmd)
		var out bytes.Buffer
		if _, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Stdout: &out, WorkDir: workDir}); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		return strings.TrimSpace(out.String())
	}
	script := func(t *testing.T, dir string) {
		t.Helper()
		writeFile(t, filepath.Join(dir, "x.sh"), "#!/bin/sh\necho ok\n")
		if err := os.Chmod(filepath.Join(dir, "x.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("pwd", func(t *testing.T) {
		dir := t.TempDir()
		got := run(t, "pwd", dir)
		g, _ := filepath.EvalSymlinks(got)
		w, _ := filepath.EvalSymlinks(dir)
		if g != w {
			t.Errorf("pwd = %q, want %q", g, w)
		}
	})
	t.Run("relative executable resolved against WorkDir", func(t *testing.T) {
		dir := t.TempDir()
		script(t, dir)
		t.Chdir(t.TempDir())
		if got := run(t, "./x.sh", dir); got != "ok" {
			t.Errorf("output = %q, want ok", got)
		}
	})
	t.Run("bare name from PATH", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "echo"), "#!/bin/sh\necho shadowed\n")
		if err := os.Chmod(filepath.Join(dir, "echo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := run(t, "echo hi", dir); got != "hi" {
			t.Errorf("output = %q, want hi (PATH must win over WorkDir)", got)
		}
	})
	t.Run("relative WorkDir", func(t *testing.T) {
		parent := t.TempDir()
		script(t, filepath.Join(parent, "sub"))
		t.Chdir(parent)
		if got := run(t, "./x.sh", "sub"); got != "ok" {
			t.Errorf("output = %q, want ok", got)
		}
	})
	t.Run("absolute executable unchanged", func(t *testing.T) {
		dir := t.TempDir()
		script(t, dir)
		if got := run(t, filepath.Join(dir, "x.sh"), t.TempDir()); got != "ok" {
			t.Errorf("output = %q, want ok", got)
		}
	})
}

func TestExecuteContextCanceled(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, "sleep 5")

	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := rt.Execute(ctx, tgt, core.ExecuteRequest{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("took %v, want < 3s", d)
		}
	})
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		_, err := rt.Execute(ctx, tgt, core.ExecuteRequest{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want Canceled", err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("took %v, want < 3s", d)
		}
	})
}

func TestExecuteDryRun(t *testing.T) {
	skipWindows(t)
	marker := filepath.Join(t.TempDir(), "touched")
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, "touch "+marker)
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{DryRun: true})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := []string{"touch", marker}; !reflect.DeepEqual(res.Argv, want) {
		t.Errorf("Argv = %v, want %v", res.Argv, want)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("command ran during dry run (stat err = %v)", err)
	}
}

func TestExecuteValidationIsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "m.yaml"), "name: m\ncommands:\n  c:\n    cmd: echo {{.x}}\n    params:\n      - name: x\n        required: true\n")
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := rt.Resolve("m", "c")
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.Execute(context.Background(), tgt, core.ExecuteRequest{})
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if want := `modulerunner: execmodule: required param "x" missing`; err.Error() != want {
		t.Errorf("text = %q, want %q", err, want)
	}
}

func TestExecuteEnv(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, `"sh -c 'echo $FUTI_MODULE_NAME $FUTI_MODULE_ROOT'"`)
	var out bytes.Buffer
	if _, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(out.String())
	if len(fields) != 2 || fields[0] != "m" || fields[1] == "" {
		t.Errorf("output = %q, want module name m and a non-empty root", out.String())
	}
}

func TestExecuteNilWriters(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, "cat")
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Execute = %+v, %v; want ExitCode 0, nil", res, err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("sink broken") }

// A writer error with child exit 0 is a hard error, not a WaitErr.
func TestExecuteWriterErrorIsError(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, "echo hi")
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Stdout: failWriter{}})
	if err == nil {
		t.Fatal("err = nil, want writer error")
	}
	if res.WaitErr != nil {
		t.Errorf("WaitErr = %v, want nil", res.WaitErr)
	}
}

// A writer error under an output limit is also a hard error, not a WaitErr.
func TestExecuteWriterErrorWithLimitIsError(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, "echo hi")
	res, err := rt.Execute(context.Background(), tgt,
		core.ExecuteRequest{Stdout: failWriter{}, OutputLimit: 100})
	if err == nil {
		t.Fatal("err = nil, want writer error")
	}
	if res.WaitErr != nil || res.ExitCode != 0 {
		t.Errorf("WaitErr = %v, ExitCode = %d; want nil, 0", res.WaitErr, res.ExitCode)
	}
}

// A grandchild holding the pipe yields exec.ErrWaitDelay: not an error.
func TestExecuteWaitDelayIsNotError(t *testing.T) {
	skipWindows(t)
	if testing.Short() {
		t.Skip("waits for executor WaitDelay")
	}
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, `"sh -c '(sleep 8) & exit 0'"`)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := rt.Execute(ctx, tgt, core.ExecuteRequest{Stdout: &out})
	if err != nil || res.WaitErr != nil || res.ExitCode != 0 {
		t.Fatalf("Execute = %+v, %v; want clean result", res, err)
	}
}

type countingLoader struct {
	mu    sync.Mutex
	calls int
}

func (c *countingLoader) LoadModule(path, root string) (*execmodule.Module, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return execmodule.LoadModule(path, root)
}

func TestResolveExecuteLoadsModuleOnce(t *testing.T) {
	skipWindows(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "m.yaml"), "name: m\ncommands:\n  c:\n    cmd: echo hi\n")
	opts := core.Options{ModulesDir: dir, Exec: core.CLIExecPolicy}
	cl := &countingLoader{}
	core.SetLoader(&opts, cl)
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := rt.Resolve("m", "c")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{}); err != nil {
		t.Fatal(err)
	}
	if cl.calls != 1 {
		t.Errorf("module loads = %d, want 1", cl.calls)
	}
}

func TestExecuteOutputLimitParallelStreams(t *testing.T) {
	skipWindows(t)
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy},
		`"sh -c 'for i in 1 2 3 4 5 6 7 8 9 10; do (printf aaaaaaaaaa >&2) & printf bbbbbbbbbb; done; wait'"`)
	var out, errb bytes.Buffer
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Stdout: &out, Stderr: &errb, OutputLimit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if out.Len()+errb.Len() != 50 || !res.Truncated {
		t.Errorf("len(out)+len(err) = %d, Truncated = %v; want 50, true", out.Len()+errb.Len(), res.Truncated)
	}
}

func TestExecuteConcurrent(t *testing.T) {
	skipWindows(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "m.yaml"), "name: m\ncommands:\n  c:\n    cmd: echo {{.i}}\n    params:\n      - name: i\n        required: true\n")
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir, Exec: core.CLIExecPolicy})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tgt, err := rt.Resolve("m", "c")
			if err != nil {
				t.Error(err)
				return
			}
			var out bytes.Buffer
			want := strconv.Itoa(i)
			if _, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Params: map[string]string{"i": want}, Stdout: &out}); err != nil {
				t.Error(err)
				return
			}
			if got := strings.TrimSpace(out.String()); got != want {
				t.Errorf("goroutine %d output = %q", i, got)
			}
		}()
	}
	wg.Wait()
}
