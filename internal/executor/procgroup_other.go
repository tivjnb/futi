//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package executor

import (
	"os"
	"os/exec"
	"syscall"
)

// startInOwnGroup на платформах без групп процессов POSIX ничего не делает
// (изоляция там и не включается: controllingTTY всегда true).
func startInOwnGroup(*exec.Cmd) {}

// signalGroup без групп процессов шлёт sig только самому процессу.
func signalGroup(p *os.Process, sig syscall.Signal) error { return p.Signal(sig) }
