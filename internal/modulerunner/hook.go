package modulerunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
)

// RunHook evaluates and runs a module hook (attach/detach).
//
// A hook is a template in the string or list form, evaluated to argv and
// executed like cmd: without a shell. A compound hook is written explicitly,
// [sh, -c, "..."]. No inputs are available; the template sees {{.module_name}},
// {{.module_root}} and {{.env.FOO}}. stdin, group and forward are set by the
// caller (executor.Config.Stdin/Group/ForwardSignals); there is no timeout.
// The process environment is the full user environment plus FUTI_MODULE_NAME
// and FUTI_MODULE_ROOT; cwd is moduleRoot. With executor.GroupIsolateWithoutTTY
// and no controlling terminal the hook runs in its own process group, so
// signals sent to futi reach the processes the hook spawns.
func RunHook(ctx context.Context, hook execmodule.CmdSpec, moduleRoot, moduleName string,
	stdin io.Reader, stdout, stderr io.Writer, group executor.GroupMode, forward bool) error {
	// An absolute root: cwd (Dir), FUTI_MODULE_ROOT and {{.module_root}}
	// must point at the same directory.
	moduleRoot, err := filepath.Abs(moduleRoot)
	if err != nil {
		return fmt.Errorf("modulerunner: %w", err)
	}
	tmpl, err := hook.Template()
	if err != nil {
		return fmt.Errorf("modulerunner: %w", err)
	}
	if errs := tmpl.Check(nil); len(errs) > 0 {
		return fmt.Errorf("modulerunner: %w", errs[0])
	}
	args, err := tmpl.Eval(nil, argvtmpl.Values{ModuleName: moduleName, ModuleRoot: moduleRoot})
	if err != nil {
		var terr *argvtmpl.Error
		if errors.As(err, &terr) && terr.Msg == argvtmpl.EmptyArgvMsg {
			return errors.New("modulerunner: hook rendered to empty argv")
		}
		return fmt.Errorf("modulerunner: %w", err)
	}
	argv := argvtmpl.Argv(args)
	// A program path with a separator is relative to the module root, like
	// the cwd of the hook; the executor would resolve it against futi's cwd.
	// A bare name is still looked up in PATH.
	argv[0] = hookProgramPath(argv[0], moduleRoot, runtime.GOOS == "windows")

	env := os.Environ()
	env = append(env,
		"FUTI_MODULE_NAME="+moduleName,
		"FUTI_MODULE_ROOT="+moduleRoot,
	)

	cfg := &executor.Config{
		Dir:            moduleRoot,
		Env:            env,
		Stdin:          stdin,
		Stdout:         stdout,
		Stderr:         stderr,
		ForwardSignals: forward,
		Group:          group,
	}
	return executor.RunWithConfig(ctx, cfg, argv[0], argv[1:]...)
}

// hookProgramPath joins a program path with a separator to the module root,
// like the cwd of the hook. A path that is not relative to the module root is
// returned as is: an absolute one, a bare name (looked up in PATH) and, with
// win, a drive-relative (C:foo\x.exe) or root-relative (\foo) one.
func hookProgramPath(prog, moduleRoot string, win bool) string {
	if win {
		if len(prog) >= 2 && prog[1] == ':' || strings.HasPrefix(prog, `\`) || strings.HasPrefix(prog, `/`) {
			return prog
		}
		if strings.ContainsAny(prog, `/\`) {
			return filepath.Join(moduleRoot, prog)
		}
		return prog
	}
	if !filepath.IsAbs(prog) && strings.Contains(prog, "/") {
		return filepath.Join(moduleRoot, prog)
	}
	return prog
}
