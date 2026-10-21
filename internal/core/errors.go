package core

import (
	"errors"
	"os/exec"
	"syscall"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// ErrNotFound is the class of errors where a module or command does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidInput is the class of errors caused by invalid caller input.
var ErrInvalidInput = errors.New("invalid input")

// classError attaches an error class to err without changing its text.
type classError struct {
	err   error
	class error
}

func (e *classError) Error() string        { return e.err.Error() }
func (e *classError) Is(target error) bool { return target == e.class }
func (e *classError) Unwrap() error        { return e.err }

// classify marks known sentinel errors with their class (ErrNotFound is
// checked first); other errors are returned as is.
func classify(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, discovery.ErrNotFound),
		errors.Is(err, modulerunner.ErrModuleNotFound),
		errors.Is(err, execmodule.ErrCommandNotFound):
		return &classError{err: err, class: ErrNotFound}
	case errors.Is(err, discovery.ErrInvalidNamespace),
		errors.Is(err, modulerunner.ErrNoDefaultCommand),
		errors.Is(err, execmodule.ErrRequiresSubcommand):
		return &classError{err: err, class: ErrInvalidInput}
	}
	return err
}

// invalidInput unconditionally marks err as ErrInvalidInput.
func invalidInput(err error) error {
	return &classError{err: err, class: ErrInvalidInput}
}

// ExitCode extracts the process exit code from err.
// If the process died from signal N (*exec.ExitError) it is 128+N (as in a
// shell); otherwise, if the command was interrupted by signal N received by
// futi (*executor.InterruptedError), also 128+N; otherwise ExitCode() of
// *exec.ExitError. Anything else is 1 (the standard futi error code).
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	hasExitErr := errors.As(err, &exitErr)
	if hasExitErr {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
	}
	var intErr *executor.InterruptedError
	if errors.As(err, &intErr) {
		return 128 + int(intErr.Signal)
	}
	if hasExitErr {
		return exitErr.ExitCode()
	}
	return 1
}
