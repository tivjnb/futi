package modules

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tivjnb/futi/internal/core"
)

// stdinIsTerminal is the test-side TTY check; setStdinTerminal replaces it.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// newManagerWithRuntime builds a Manager over rt with stderr/stdin of the
// process and the given stdout and prompt input.
func newManagerWithRuntime(w io.Writer, stdin io.Reader, rt *core.Runtime) *Manager {
	return New(rt, Streams{
		Prompt: NewPrompter(stdin, func() bool { return stdinIsTerminal() }),
		Out:    w,
		Err:    os.Stderr,
		Stdin:  os.Stdin,
	})
}

// newTestManager builds a Manager over newTestRuntime with process stderr/stdin
// and the given stdout and prompt input.
func newTestManager(t *testing.T, out io.Writer, stdin io.Reader) *Manager {
	t.Helper()
	return New(newTestRuntime(t), Streams{
		Prompt: NewPrompter(stdin, func() bool { return stdinIsTerminal() }),
		Out:    out,
		Err:    os.Stderr,
		Stdin:  os.Stdin,
	})
}
