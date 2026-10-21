//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package modulerunner

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/tivjnb/futi/internal/executor"
)

// Хук без управляющего терминала (CI, cron, ssh без tty) — в своей группе
// процессов, чтобы сигналы futi доходили до всех его процессов. Наблюдаемо,
// только если у тестового процесса нет управляющего терминала (executor
// проверяет его сам).
func TestRunHookIsolatedWithoutTTY(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("нужен /proc")
	}
	if tty, err := os.Open("/dev/tty"); err == nil {
		tty.Close()
		t.Skip("есть управляющий терминал: хук не изолируется")
	}
	var out bytes.Buffer
	if err := RunHook(context.Background(), `echo $$ $(cut -d" " -f5 /proc/$$/stat)`, t.TempDir(), "m", nil, &out, io.Discard, executor.GroupIsolateWithoutTTY, true); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	fields := strings.Fields(out.String())
	if len(fields) != 2 {
		t.Fatalf("неожиданный вывод: %q", out.String())
	}
	if fields[1] != fields[0] {
		t.Fatalf("pgrp хука = %s, want %s (своя группа; группа теста %d)", fields[1], fields[0], syscall.Getpgrp())
	}
}
