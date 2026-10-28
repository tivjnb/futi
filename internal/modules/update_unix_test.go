//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package modules

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tivjnb/futi/internal/executor"
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
	for _, src := range []string{srcA, srcB} {
		if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: src, SkipAttach: true}); err != nil {
			t.Fatalf("install: %v", err)
		}
	}
	ready := filepath.Join(t.TempDir(), "ready")
	attach := `trap "exit 0" TERM; echo ready > ` + ready + `; i=0; while [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done`
	writeFile(t, filepath.Join(srcA, "config.yaml"),
		"name: a\nattach: '"+attach+"'\ncommands:\n  run:\n    cmd: echo v2\n")
	writeFile(t, filepath.Join(srcB, "config.yaml"),
		"name: b\ncommands:\n  run:\n    cmd: echo v2\n")

	m := newTestManager(t, &bytes.Buffer{}, nil)
	done := make(chan error, 1)
	go func() {
		done <- m.Update(context.Background(), UpdateOptions{Yes: true})
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Update не завершился")
	}
	if sig, ok := executor.InterruptSignal(err); !ok || sig != syscall.SIGTERM {
		t.Fatalf("executor.InterruptSignal(%v) = (%v, %v), want (SIGTERM, true)", err, sig, ok)
	}
	data, _ := os.ReadFile(filepath.Join(global, "b", "config.yaml"))
	if strings.Contains(string(data), "echo v2") {
		t.Fatal("модуль b не должен обновляться после прерывания a")
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
	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: src}); err != nil {
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

	if err := newTestManager(t, &bytes.Buffer{}, nil).Update(context.Background(), UpdateOptions{Name: "qr", Yes: true}); err == nil {
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
	if err := newTestManager(t, &b, nil).Update(context.Background(), UpdateOptions{Name: "qr", Yes: true}); err != nil {
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
	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: src}); err != nil {
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

	err := newTestManager(t, &bytes.Buffer{}, nil).Update(context.Background(), UpdateOptions{Name: "qr", Yes: true})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), dest+".futi-backup") {
		t.Fatalf("want rollback failure naming backup path, got %v", err)
	}
}

// crossDeviceRename форсирует путь копирования в moveTree, как при EXDEV.
func crossDeviceRename(t *testing.T) {
	t.Helper()
	orig := renameDir
	renameDir = func(string, string) error { return syscall.EXDEV }
	t.Cleanup(func() { renameDir = orig })
}

// Копирование в moveTree переносит дерево как rename: симлинки — симлинками
// с той же целью, бит исполнения сохраняется.
func TestMoveTreeFallbackKeepsSymlinks(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	writeFile(t, filepath.Join(src, "run.sh"), "echo\n")
	if err := os.Chmod(filepath.Join(src, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("run.sh", filepath.Join(src, "current")); err != nil {
		t.Fatal(err)
	}
	crossDeviceRename(t)

	dst := filepath.Join(t.TempDir(), "dst")
	if err := moveTree(src, dst); err != nil {
		t.Fatalf("moveTree: %v", err)
	}
	fi, err := os.Lstat(filepath.Join(dst, "current"))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("current must be a symlink: fi=%v err=%v", fi, err)
	}
	if target, _ := os.Readlink(filepath.Join(dst, "current")); target != "run.sh" {
		t.Fatalf("symlink target = %q, want run.sh", target)
	}
	if fi, err := os.Stat(filepath.Join(dst, "run.sh")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("run.sh mode = %v (err %v), want 0755", fi.Mode().Perm(), err)
	}
}

// Модуль из git с симлинком, установленный через копирование (другая ФС):
// update без изменений апстрима — up to date, без вопроса о замене.
func TestUpdateCrossDeviceSymlinkUpToDate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	global := attachEnv(t)
	setStdinTerminal(t, false)
	repo := filepath.Join(t.TempDir(), "qr")
	writeFile(t, filepath.Join(repo, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo run\n")
	if err := os.Symlink("config.yaml", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	gitRunTest(t, repo, "init", "-q")
	gitRunTest(t, repo, "add", ".")
	gitRunTest(t, repo, "commit", "-q", "-m", "init")
	crossDeviceRename(t)
	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: repo, FromGit: true}); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, stderr, err := runUpdateQR(t, unreadableStdin{t})
	if err != nil || out != "qr: up to date\n" {
		t.Fatalf("update: out=%q err=%v stderr=%q, want up to date", out, err, stderr)
	}
	if fi, err := os.Lstat(filepath.Join(global, "qr", "link")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link must stay a symlink in dest: err=%v", err)
	}
}

// M3(б): модуль из git, в апстриме поменялась только цель симлинка — это
// изменение (M), а не up to date. Локальные источники симлинки не копируют.
func TestUpdateSymlinkTargetOnlyChangeShownAsModified(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	global := attachEnv(t)
	setStdinTerminal(t, false)
	repo := filepath.Join(t.TempDir(), "qr")
	writeFile(t, filepath.Join(repo, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo run\n")
	link := filepath.Join(repo, "lnk")
	if err := os.Symlink("a.txt", link); err != nil {
		t.Fatal(err)
	}
	gitRunTest(t, repo, "init", "-q")
	gitRunTest(t, repo, "add", ".")
	gitRunTest(t, repo, "commit", "-q", "-m", "init")
	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: repo, FromGit: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b.txt", link); err != nil {
		t.Fatal(err)
	}
	gitRunTest(t, repo, "commit", "-q", "-a", "-m", "retarget")

	out, stderr, err := runUpdateQR(t, unreadableStdin{t}, "--yes")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if out != "qr: updated\n" || !strings.Contains(stderr, "):\n  M lnk\n") {
		t.Fatalf("out = %q, stderr = %q; want updated with M lnk", out, stderr)
	}
	if got, _ := os.Readlink(filepath.Join(global, "qr", "lnk")); got != "b.txt" {
		t.Fatalf("link target = %q, want b.txt", got)
	}
}
