//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package executor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// startInOwnGroup запускает command в новой группе процессов с pgid, равным
// pid ребёнка.
func startInOwnGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup шлёт sig всей группе процессов, лидер которой — p. Группа уже
// пуста (ESRCH) — os.ErrProcessDone. После reap лидера pgid теоретически может
// достаться новой группе: защиты вроде pidfd для групп нет, но pgid занят,
// пока в группе есть хоть один процесс, и риск переиспользования пренебрежим.
func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
