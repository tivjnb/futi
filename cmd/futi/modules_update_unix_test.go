//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"futi/internal/discovery"
	"futi/internal/testutil"
)

// Хук, перехвативший пересланный futi SIGTERM и вышедший с 0, всё равно
// прерывает обновление всех модулей: сигнал получил futi.
func TestRunModulesUpdateStopsWhenHookCatchesSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	parent := t.TempDir()
	srcA := makeSourceModule(t, parent, "a", "")
	srcB := makeSourceModule(t, parent, "b", "")
	cache := newTestCache()
	for _, src := range []string{srcA, srcB} {
		if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src, "--skip-attach"}); err != nil {
			t.Fatalf("install: %v", err)
		}
	}
	ready := filepath.Join(t.TempDir(), "ready")
	attach := `trap "exit 0" TERM; echo ready > ` + ready + `; i=0; while [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done`
	writeFile(t, filepath.Join(srcA, "config.yaml"),
		"name: a\nattach: '"+attach+"'\ncommands:\n  run:\n    cmd: echo v2\n")
	writeFile(t, filepath.Join(srcB, "config.yaml"),
		"name: b\ncommands:\n  run:\n    cmd: echo v2\n")

	done := make(chan error, 1)
	go func() {
		done <- runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, nil)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runModulesUpdate не завершился")
	}
	if sig, ok := interruptSignal(err); !ok || sig != syscall.SIGTERM {
		t.Fatalf("interruptSignal(%v) = (%v, %v), want (SIGTERM, true)", err, sig, ok)
	}
	data, _ := os.ReadFile(filepath.Join(global, "b", "config.yaml"))
	if strings.Contains(string(data), "echo v2") {
		t.Fatal("модуль b не должен обновляться после прерывания a")
	}
}
