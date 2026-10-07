//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/testutil"
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

// Хук, завершившийся ненулевым кодом (не сигналом), даёт код выхода futi 1,
// а не код хука: README обещает 1 для прочих ошибок modules-команд.
func TestFutiModulesInstallFailedAttachExitsWithOne(t *testing.T) {
	tmp := t.TempDir()
	src := makeSourceModule(t, t.TempDir(), "a", "attach: \"exit 3\"\n")
	cmd := exec.Command(builtFutiBinary(t), "modules", "install", src)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"FUTI_MODULES_DIR="+filepath.Join(tmp, "mods"),
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err == nil {
		t.Fatal("ожидалась ошибка attach")
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Fatalf("код выхода = %d, want 1", code)
	}
}

// failingCopyRename подменяет renameDir: перенос идёт через копирование, а
// файл z.txt в источнике становится нечитаемым — копирование падает посреди
// (a.txt к этому моменту уже скопирован). Возвращает функцию-«починку».
func failingCopyRename(t *testing.T) (fix func()) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root читает файлы с правами 000")
	}
	orig := renameDir
	renameDir = func(src, dst string) error {
		if err := os.Chmod(filepath.Join(src, "z.txt"), 0o000); err != nil {
			t.Fatal(err)
		}
		return syscall.EXDEV
	}
	t.Cleanup(func() { renameDir = orig })
	return func() { renameDir = orig }
}

func TestMoveTreeCopyFailureRemovesDst(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "a\n")
	writeFile(t, filepath.Join(src, "z.txt"), "z\n")
	failingCopyRename(t)

	dst := filepath.Join(t.TempDir(), "dst")
	if err := moveTree(src, dst); err == nil {
		t.Fatal("moveTree must fail when a file is unreadable")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("partial dst must be removed, stat err = %v", err)
	}
}

func TestRunModulesUpdateCopyFailureRestoresOld(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	dest := filepath.Join(global, "qr")
	oldMeta, err := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	if err != nil {
		t.Fatal(err)
	}
	oldConfig, err := os.ReadFile(filepath.Join(dest, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo v2\n")
	writeFile(t, filepath.Join(src, "a.txt"), "a\n")
	writeFile(t, filepath.Join(src, "z.txt"), "z\n")
	fix := failingCopyRename(t)

	source := discovery.NewGlobalSource(global)
	if err := runModulesUpdate(&bytes.Buffer{}, source, cache, []string{"qr"}); err == nil {
		t.Fatal("update must fail when the copy fails")
	}

	gotConfig, err := os.ReadFile(filepath.Join(dest, "config.yaml"))
	if err != nil || string(gotConfig) != string(oldConfig) {
		t.Fatalf("old tree must be restored: err=%v config=%q", err, gotConfig)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("partial new tree must not remain, stat err = %v", err)
	}
	gotMeta, err := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	if err != nil || gotMeta.Lockfile.Hash != oldMeta.Lockfile.Hash {
		t.Fatalf("old meta must be restored: err=%v meta=%+v", err, gotMeta)
	}
	if _, err := os.Stat(dest + ".futi-backup"); !os.IsNotExist(err) {
		t.Fatalf("backup must not remain, stat err = %v", err)
	}

	// Повторный update после «исправления» (перенос работает) проходит.
	fix()
	var b bytes.Buffer
	if err := runModulesUpdate(&b, source, cache, []string{"qr"}); err != nil {
		t.Fatalf("repeated update: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dest, "config.yaml"))
	if !strings.Contains(string(data), "echo v2") {
		t.Fatalf("new tree must be installed:\n%s", data)
	}
}

func TestMoveTreeExistingDstUntouched(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "new\n")
	dst := t.TempDir()
	writeFile(t, filepath.Join(dst, "keep.txt"), "keep\n")

	if err := moveTree(src, dst); err == nil {
		t.Fatal("moveTree must refuse an existing dst")
	}
	data, err := os.ReadFile(filepath.Join(dst, "keep.txt"))
	if err != nil || string(data) != "keep\n" {
		t.Fatalf("existing dst must be untouched: err=%v data=%q", err, data)
	}
}

// Сбой самого отката: ошибка называет путь backup, где осталась рабочая копия.
func TestRunModulesUpdateRollbackFailureNamesBackup(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	dest := filepath.Join(global, "qr")
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo v2\n")
	writeFile(t, filepath.Join(src, "a.txt"), "a\n")
	writeFile(t, filepath.Join(src, "z.txt"), "z\n")
	failingCopyRename(t)
	// Копирование падает, а backup к моменту отката исчезает — Rename назад упадёт.
	base := renameDir
	renameDir = func(src, dst string) error {
		if err := os.RemoveAll(dest + ".futi-backup"); err != nil {
			t.Fatal(err)
		}
		return base(src, dst)
	}

	err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, []string{"qr"})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), dest+".futi-backup") {
		t.Fatalf("want rollback failure naming backup path, got %v", err)
	}
}
