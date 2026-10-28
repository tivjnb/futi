package executor

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

func TestInterruptSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	killed := func(sig string) error {
		return exec.Command("sh", "-c", "kill -"+sig+" $$").Run()
	}
	cases := []struct {
		name    string
		err     error
		want    syscall.Signal
		wantHit bool
	}{
		{"INT", killed("INT"), syscall.SIGINT, true},
		{"TERM wrapped", fmt.Errorf("x: %w", killed("TERM")), syscall.SIGTERM, true},
		{"HUP", killed("HUP"), syscall.SIGHUP, true},
		{"QUIT не прерывающий", killed("QUIT"), 0, false},
		{"обычный код", exec.Command("sh", "-c", "exit 3").Run(), 0, false},
		{"не ExitError", errors.New("boom"), 0, false},
		{"nil", nil, 0, false},
		{"InterruptedError TERM", &InterruptedError{Signal: syscall.SIGTERM}, syscall.SIGTERM, true},
		{"InterruptedError QUIT не прерывающий", &InterruptedError{Signal: syscall.SIGQUIT}, 0, false},
		{"InterruptedError HUP wrapped", fmt.Errorf("x: %w", &InterruptedError{Signal: syscall.SIGHUP}), syscall.SIGHUP, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := InterruptSignal(tc.err)
			if ok != tc.wantHit || got != tc.want {
				t.Fatalf("InterruptSignal(%v) = (%v, %v), want (%v, %v)", tc.err, got, ok, tc.want, tc.wantHit)
			}
		})
	}
}
