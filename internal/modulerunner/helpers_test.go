package modulerunner

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/executor"
)

// resolveForTest resolves namespace and command, the old two-part form, via
// ResolveWords: namespace segments, then command if it is not empty.
func resolveForTest(source discovery.Source, loader ModuleLoader, namespace, command string) (*Resolved, error) {
	words := strings.Split(namespace, ".")
	if command != "" {
		words = append(words, command)
	}
	r, _, err := ResolveWords(source, loader, words)
	return r, err
}

// prepareForTest resolves, validates and renders a command in one step.
func prepareForTest(
	source discovery.Source,
	namespace, command string,
	inputs map[string]any,
	loader ModuleLoader,
) (*Prepared, error) {
	r, err := resolveForTest(source, loader, namespace, command)
	if err != nil {
		return nil, err
	}
	vals, err := r.Validate(inputs)
	if err != nil {
		return nil, err
	}
	return r.Prepare(vals)
}

// runForTest prepares a command and runs it with the same executor binding
// the CLI uses. nil stdout/stderr fall back to os.Stdout/os.Stderr.
func runForTest(
	ctx context.Context,
	source discovery.Source,
	namespace, command string,
	inputs map[string]any,
	stdout, stderr io.Writer,
	loader ModuleLoader,
) error {
	p, err := prepareForTest(source, namespace, command, inputs, loader)
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
