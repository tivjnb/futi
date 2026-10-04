//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package executor

import (
	"os"
	"syscall"
	"unsafe"
)

// isForegroundTTY: futi в foreground-группе управляющего терминала, если для
// первого из fd 2, 1, 0, являющегося терминалом, группа переднего плана
// терминала совпадает с группой futi. Если все три перенаправлены,
// проверяется /dev/tty; не открылся — терминала нет.
func isForegroundTTY() bool {
	for _, fd := range []uintptr{2, 1, 0} {
		if pgrp, ok := terminalPgrp(fd); ok {
			return pgrp == syscall.Getpgrp()
		}
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	pgrp, ok := terminalPgrp(tty.Fd())
	return ok && pgrp == syscall.Getpgrp()
}

// terminalPgrp возвращает группу переднего плана терминала fd
// (ioctl TIOCGPGRP); false — fd не управляющий терминал.
func terminalPgrp(fd uintptr) (int, bool) {
	var pgrp int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&pgrp)))
	if errno != 0 {
		return 0, false
	}
	return int(pgrp), true
}
