//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package core_test

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/executor"
)

// sigtermWhenReady sends SIGTERM to the test process once ready is closed and
// returns the channel on which the test itself observes the signal.
func sigtermWhenReady(t *testing.T, ready <-chan struct{}) <-chan os.Signal {
	t.Helper()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(ch) })
	go func() {
		<-ready
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	return ch
}

type readyWriter struct {
	once  sync.Once
	ready chan struct{}
}

func (w *readyWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ready) })
	return len(p), nil
}

func TestExecuteCLIPolicyForwardsSignals(t *testing.T) {
	rt, tgt := newExecRT(t, core.Options{Exec: core.CLIExecPolicy}, `"sh -c 'echo ready; sleep 5'"`)
	w := &readyWriter{ready: make(chan struct{})}
	sigtermWhenReady(t, w.ready)
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Stdout: w})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var intr *executor.InterruptedError
	if !errors.As(res.WaitErr, &intr) || res.ExitCode != 143 {
		t.Errorf("WaitErr = %v, ExitCode = %d; want InterruptedError, 143", res.WaitErr, res.ExitCode)
	}
}

func TestExecuteServerPolicyNoForward(t *testing.T) {
	rt, tgt := newExecRT(t, core.Options{Exec: core.ServerExecPolicy}, `"sh -c 'echo ready; sleep 1; exit 0'"`)
	w := &readyWriter{ready: make(chan struct{})}
	got := sigtermWhenReady(t, w.ready)
	res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Stdout: w})
	// The signal was sent while the child slept; it must have reached the
	// test process, otherwise "not interrupted" below proves nothing.
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM was never delivered to the test process")
	}
	if err != nil || res.WaitErr != nil || res.ExitCode != 0 {
		t.Errorf("Execute = %+v, %v; want clean result", res, err)
	}
}

func TestExecuteServerPolicyCancelKillsGrandchild(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for executor cancelWaitDelay")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	rt, tgt := newExecRT(t, core.Options{Exec: core.ServerExecPolicy},
		`"sh -c 'sh -c \"trap \\\"\\\" TERM; echo \\$\\$ > `+pidFile+`; while :; do sleep 1; done\" & wait'"`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if b, _ := os.ReadFile(pidFile); strings.TrimSpace(string(b)) != "" {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	_, err := rt.Execute(ctx, tgt, core.ExecuteRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
	b, _ := os.ReadFile(pidFile)
	pid, perr := strconv.Atoi(strings.TrimSpace(string(b)))
	if perr != nil {
		t.Fatalf("pid file: %q", b)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild %d still alive after cancel", pid)
}
