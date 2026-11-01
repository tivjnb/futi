package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// ExecPolicy describes how child processes are run for a given front end.
type ExecPolicy struct {
	ForwardSignals bool
	CommandGroup   executor.GroupMode // module commands, manager git/curl
	HookGroup      executor.GroupMode // attach/detach hooks
}

// CLIExecPolicy is the policy of the interactive CLI.
var CLIExecPolicy = ExecPolicy{
	ForwardSignals: true,
	CommandGroup:   executor.GroupInherit,
	HookGroup:      executor.GroupIsolateWithoutTTY,
}

// ServerExecPolicy is the policy of a long-running server without a terminal.
var ServerExecPolicy = ExecPolicy{
	ForwardSignals: false,
	CommandGroup:   executor.GroupAlways,
	HookGroup:      executor.GroupAlways,
}

// Target is a resolved module command.
type Target struct{ r *modulerunner.Resolved }

// Command returns the resolved command.
func (t *Target) Command() *execmodule.Command { return t.r.Command }

// ModuleName returns the effective module name.
func (t *Target) ModuleName() string { return t.r.ModuleName() }

// Manifest returns the path of the module manifest.
func (t *Target) Manifest() string { return t.r.ManifestPath }

// ResolveWords walks the command path at the start of words over the merged
// module tree and returns the runnable command and the words after the path.
// Not-found and invalid-input failures carry ErrNotFound and ErrInvalidInput.
func (r *Runtime) ResolveWords(words []string) (*Target, []string, error) {
	src, err := r.Source()
	if err != nil {
		return nil, nil, err
	}
	res, rest, err := modulerunner.ResolveWords(src, r.loader(), words)
	if err != nil {
		return nil, nil, classify(err)
	}
	return &Target{r: res}, rest, nil
}

// ExecuteRequest describes one command run.
type ExecuteRequest struct {
	// Inputs holds the given args and flags by name: string, bool, []string
	// or []any of strings (see inputs.Bind).
	Inputs map[string]any
	// Stdin is the child's stdin; nil means empty.
	Stdin io.Reader
	// Stdout and Stderr receive the output; nil discards it.
	Stdout, Stderr io.Writer
	// WorkDir is the child's working directory; empty means the cwd of futi.
	WorkDir string
	// OutputLimit caps the total bytes written to Stdout and Stderr; 0 means
	// no limit. Excess output is dropped and ExecuteResult.Truncated is set.
	OutputLimit int64
	// DryRun renders the command without running it.
	DryRun bool
}

// ExecuteResult describes a finished (or dry-run) command.
type ExecuteResult struct {
	// ExitCode is the child's exit code (128+N for signals); 0 for dry runs.
	ExitCode int
	// WaitErr is the child's non-zero or signal termination, if any. It is
	// not an Execute error.
	WaitErr error
	// Truncated reports that output was dropped by OutputLimit.
	Truncated bool
	// Argv is the rendered argv, without WorkDir substitution.
	Argv []string
	// Module is the path of the module manifest.
	Module                string
	StartedAt, FinishedAt time.Time
}

// Execute validates the request and runs the target command. A child that
// exits non-zero is not an error: see ExecuteResult.WaitErr and ExitCode.
// Errors are invalid input (ErrInvalidInput), failures to start or to write
// output, and ctx cancellation (wrapping ctx.Err(), which takes priority).
func (r *Runtime) Execute(ctx context.Context, t *Target, req ExecuteRequest) (ExecuteResult, error) {
	var res ExecuteResult
	vals, err := t.r.Validate(req.Inputs)
	if err != nil {
		return res, invalidInput(err)
	}
	p, err := t.r.Prepare(vals)
	if err != nil {
		var dash *argvtmpl.DashError
		if errors.As(err, &dash) {
			return res, invalidInput(err)
		}
		return res, err
	}
	res.Argv = p.Argv
	res.Module = t.Manifest()
	if req.DryRun {
		return res, nil
	}

	argv := append([]string(nil), p.Argv...)
	workDir := req.WorkDir
	if workDir != "" {
		// Relative WorkDir would otherwise apply twice: Join + cmd.Dir.
		if workDir, err = filepath.Abs(workDir); err != nil {
			return res, fmt.Errorf("execute: %w", err)
		}
		// A relative path with a separator is resolved by the OS against the
		// child's Dir, while LookPath resolves it against futi's cwd; make
		// both agree. Bare names are looked up in PATH.
		if strings.ContainsRune(argv[0], '/') || strings.ContainsRune(argv[0], filepath.Separator) {
			if !filepath.IsAbs(argv[0]) {
				argv[0] = filepath.Join(workDir, argv[0])
			}
		}
	}

	if err := batchCheck(runtime.GOOS, exec.LookPath, argv[0], p.Args); err != nil {
		return res, invalidInput(err)
	}

	stdout, stderr, limiter := outputWriters(req.Stdout, req.Stderr, req.OutputLimit)
	cfg := &executor.Config{
		Dir:            workDir,
		Env:            p.Env(),
		Stdin:          req.Stdin,
		Stdout:         stdout,
		Stderr:         stderr,
		ForwardSignals: r.opts.Exec.ForwardSignals,
		Group:          r.opts.Exec.CommandGroup,
	}
	res.StartedAt = time.Now()
	runErr := executor.RunWithConfig(ctx, cfg, argv[0], argv[1:]...)
	res.FinishedAt = time.Now()

	waitErr, code, fatal := splitRunError(runErr)
	res.WaitErr, res.ExitCode = waitErr, code
	if limiter != nil {
		res.Truncated = limiter.Truncated()
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("execute: %w", ctx.Err())
	}
	return res, fatal
}

// batchCheck refuses input values that cmd.exe would interpret when argv0
// resolves to a .bat or .cmd file. It only applies on Windows. A failed
// lookup is skipped: the executor reports it when starting the command.
func batchCheck(goos string, look func(string) (string, error), argv0 string, args []argvtmpl.Arg) error {
	if goos != "windows" {
		return nil
	}
	exe, err := look(argv0)
	if err != nil {
		return nil
	}
	return argvtmpl.CheckBatch(exe, args)
}

// splitRunError separates the outcome of executor.RunWithConfig: a child
// termination (non-zero exit, signal) is a waitErr with its exit code;
// exec.ErrWaitDelay (a grandchild holding the output pipes) is not an error;
// anything else (lookup, start, writer failure) is fatal.
func splitRunError(err error) (waitErr error, code int, fatal error) {
	if err == nil {
		return nil, 0, nil
	}
	var intErr *executor.InterruptedError
	var exitErr *exec.ExitError
	if errors.As(err, &intErr) || errors.As(err, &exitErr) {
		return err, ExitCode(err), nil
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return nil, 0, nil
	}
	return nil, 0, err
}
