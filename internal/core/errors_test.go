package core

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/modulerunner"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name     string
		sentinel error
		wrapped  error
		want     error
	}{
		{"discovery not found", discovery.ErrNotFound, fmt.Errorf("modulerunner: %w", discovery.ErrNotFound), ErrNotFound},
		{"module not found", modulerunner.ErrModuleNotFound, fmt.Errorf("modulerunner: %w", modulerunner.ErrModuleNotFound), ErrNotFound},
		{"command not found", execmodule.ErrCommandNotFound, fmt.Errorf("modulerunner: %w", execmodule.ErrCommandNotFound), ErrNotFound},
		{"invalid namespace", discovery.ErrInvalidNamespace, fmt.Errorf("modulerunner: %w", discovery.ErrInvalidNamespace), ErrInvalidInput},
		{"no default command", modulerunner.ErrNoDefaultCommand, fmt.Errorf("modulerunner: %w", modulerunner.ErrNoDefaultCommand), ErrInvalidInput},
		{"requires subcommand", execmodule.ErrRequiresSubcommand, fmt.Errorf("modulerunner: %w", execmodule.ErrRequiresSubcommand), ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.wrapped)
			if !errors.Is(got, tc.want) {
				t.Errorf("classify(%v): errors.Is(class %v) = false", tc.wrapped, tc.want)
			}
			if got.Error() != tc.wrapped.Error() {
				t.Errorf("classify(%v).Error() = %q, want %q", tc.wrapped, got.Error(), tc.wrapped.Error())
			}
			if !errors.Is(got, tc.sentinel) {
				t.Errorf("classify(%v): errors.Is(sentinel %v) = false", tc.wrapped, tc.sentinel)
			}
		})
	}
}

func TestClassifyOtherAndNil(t *testing.T) {
	if got := classify(nil); got != nil {
		t.Errorf("classify(nil) = %v, want nil", got)
	}
	other := errors.New("boom")
	got := classify(other)
	if got != other {
		t.Errorf("classify(other) = %v, want the same error", got)
	}
	if errors.Is(got, ErrNotFound) || errors.Is(got, ErrInvalidInput) {
		t.Errorf("classify(other) got a class: %v", got)
	}
}

func TestInvalidInput(t *testing.T) {
	src := errors.New("bad param")
	got := invalidInput(src)
	if !errors.Is(got, ErrInvalidInput) || errors.Is(got, ErrNotFound) {
		t.Errorf("invalidInput class wrong: %v", got)
	}
	if got.Error() != "bad param" {
		t.Errorf("invalidInput text = %q, want %q", got.Error(), "bad param")
	}
	if !errors.Is(got, src) {
		t.Error("invalidInput does not unwrap to source")
	}
}

func TestExitCodeExtractsExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	runErr := exec.Command("sh", "-c", "exit 42").Run()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"direct exit error", runErr, 42},
		{"wrapped (executor style)", fmt.Errorf("executor: run %q: %w", "sh", runErr), 42},
		{"double-wrapped (modulerunner style)", fmt.Errorf("modulerunner: %w", fmt.Errorf("executor: run %q: %w", "sh", runErr)), 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := ExitCode(tc.err); code != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, code)
			}
		})
	}
}

func TestExitCodeDefaultsToOne(t *testing.T) {
	if code := ExitCode(errors.New("something went wrong")); code != 1 {
		t.Fatalf("plain error: expected 1, got %d", code)
	}
	if code := ExitCode(nil); code != 0 {
		t.Fatalf("nil: expected 0, got %d", code)
	}
}

func TestExitCodeSignaled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals")
	}
	runErr := exec.Command("sh", "-c", "kill -TERM $$").Run()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"direct", runErr, 143},
		{"wrapped", fmt.Errorf("x: %w", runErr), 143},
		// The child caught the forwarded SIGQUIT and exited 0: code as if killed by it.
		{"InterruptedError QUIT", &executor.InterruptedError{Signal: syscall.SIGQUIT}, 131},
		// The child exited 2 after the forwarded SIGQUIT: code by the signal futi got.
		{"InterruptedError QUIT with exit 2", &executor.InterruptedError{Signal: syscall.SIGQUIT, Err: exec.Command("sh", "-c", "exit 2").Run()}, 131},
		// The child was finished off by SIGKILL after the forwarded SIGTERM: code by its death.
		{"InterruptedError TERM, killed by KILL", &executor.InterruptedError{Signal: syscall.SIGTERM, Err: exec.Command("sh", "-c", "kill -KILL $$").Run()}, 137},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.want {
				t.Fatalf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
