//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package executor

// isForegroundTTY на платформах без проверки TIOCGPGRP считает, что терминал
// (консоль) сам доставляет прерывание всем процессам, — SIGINT/SIGQUIT не
// пересылаются (прежнее поведение).
func isForegroundTTY() bool { return true }
