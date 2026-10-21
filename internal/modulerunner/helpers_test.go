package modulerunner

import (
	"context"
	"io"
	"os"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/executor"
)

// prepareForTest resolves, validates and renders a command in one step.
func prepareForTest(
	source discovery.Source,
	namespace, command string,
	params map[string]string,
	argsList []string,
	loader ModuleLoader,
) (*Prepared, error) {
	r, err := Resolve(source, loader, namespace, command)
	if err != nil {
		return nil, err
	}
	resolved, err := r.Validate(params, argsList)
	if err != nil {
		return nil, err
	}
	return r.Prepare(resolved, argsList)
}

// runForTest prepares a command and runs it with the same executor binding
// the CLI uses. nil stdout/stderr fall back to os.Stdout/os.Stderr.
func runForTest(
	ctx context.Context,
	source discovery.Source,
	namespace, command string,
	params map[string]string,
	argsList []string,
	stdout, stderr io.Writer,
	loader ModuleLoader,
) error {
	p, err := prepareForTest(source, namespace, command, params, argsList, loader)
	if err != nil {
		return err
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cfg := &executor.Config{
		Env:            p.Env(),
		Stdin:          os.Stdin,
		Stdout:         stdout,
		Stderr:         stderr,
		ForwardSignals: true,
		Group:          executor.GroupInherit,
	}
	return executor.RunWithConfig(ctx, cfg, p.Argv[0], p.Argv[1:]...)
}
