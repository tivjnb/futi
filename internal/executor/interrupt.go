package executor

import (
	"errors"
	"os/exec"
	"syscall"
)

// InterruptSignal returns the interrupting signal (SIGINT, SIGTERM or SIGHUP)
// carried by err: either one received by futi and forwarded to the command
// (*InterruptedError, even if the command caught it and exited normally) or
// one that killed the child process (*exec.ExitError). Other signals (SIGQUIT,
// SIGSEGV, ...) and other errors yield false.
func InterruptSignal(err error) (syscall.Signal, bool) {
	var intErr *InterruptedError
	if errors.As(err, &intErr) && isInterruptSignal(intErr.Signal) {
		return intErr.Signal, true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || !isInterruptSignal(ws.Signal()) {
		return 0, false
	}
	return ws.Signal(), true
}

// isInterruptSignal reports whether sig interrupts futi and shell loops.
func isInterruptSignal(sig syscall.Signal) bool {
	switch sig {
	case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
		return true
	}
	return false
}
