package core

import (
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

// Resolve finds the command for namespace and command. Not-found and
// invalid-input failures carry ErrNotFound and ErrInvalidInput.
func (r *Runtime) Resolve(namespace, command string) (*Target, error) {
	src, err := r.Source()
	if err != nil {
		return nil, err
	}
	var loader modulerunner.ModuleLoader = r.opts.loader
	if loader == nil {
		loader = r.Cache()
	}
	res, err := modulerunner.Resolve(src, loader, namespace, command)
	if err != nil {
		return nil, classify(err)
	}
	return &Target{r: res}, nil
}
