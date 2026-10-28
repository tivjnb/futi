package main

import (
	"io"
	"os"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/modules"
)

// newTestRuntime builds a Runtime from the test environment (FUTI_MODULES_DIR,
// HOME/XDG_CONFIG_HOME, cwd) as the CLI does, but with an in-memory cache.
func newTestRuntime(t *testing.T) *core.Runtime {
	t.Helper()
	opts, err := core.DefaultOptions()
	if err != nil {
		t.Fatalf("DefaultOptions: %v", err)
	}
	opts.CachePath = ""
	opts.CachePathErr = nil
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

// newTestManager builds a Manager over newTestRuntime with process stderr/stdin
// and the given stdout and prompt input.
func newTestManager(t *testing.T, out io.Writer, stdin io.Reader) *Manager {
	t.Helper()
	return New(newTestRuntime(t), modules.Streams{
		Prompt: modules.NewPrompter(stdin, func() bool { return stdinIsTerminal() }),
		Out:    out,
		Err:    os.Stderr,
		Stdin:  os.Stdin,
	})
}
