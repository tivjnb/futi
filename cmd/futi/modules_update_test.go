package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"futi/internal/discovery"
	"futi/internal/metayaml"
	"futi/internal/testutil"
)

func TestRunModulesUpdateNoChange(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Файл, созданный ПОСЛЕ установки (локальная модификация — не источник).
	dest := filepath.Join(global, "qr")
	writeFile(t, filepath.Join(dest, "local_mod.txt"), "keep\n")

	var b bytes.Buffer
	if err := runModulesUpdate(&b, discovery.NewGlobalSource(global), cache, []string{"qr"}); err != nil {
		t.Fatalf("runModulesUpdate: %v", err)
	}
	if !strings.Contains(b.String(), "up to date") {
		t.Fatalf("unexpected output: %s", b.String())
	}
	// Дерево не тронуто.
	if _, err := os.Stat(filepath.Join(dest, "local_mod.txt")); err != nil {
		t.Fatal("tree must be untouched when up to date")
	}
	// last_check обновлён.
	meta, _ := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	if meta.Lockfile.LastCheck.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("last_check must be updated")
	}
}

func TestRunModulesUpdateSourceChanged(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Источник изменился.
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo v2\n")

	oldMeta, _ := metayaml.Read(filepath.Join(global, "qr", metayaml.FileName))
	oldInstalledAt := oldMeta.Lockfile.InstalledAt

	var b bytes.Buffer
	if err := runModulesUpdate(&b, discovery.NewGlobalSource(global), cache, []string{"qr"}); err != nil {
		t.Fatalf("runModulesUpdate: %v", err)
	}
	if !strings.Contains(b.String(), "updated") {
		t.Fatalf("unexpected output: %s", b.String())
	}

	data, _ := os.ReadFile(filepath.Join(global, "qr", "config.yaml"))
	if !strings.Contains(string(data), "echo v2") {
		t.Fatalf("new tree must be installed:\n%s", string(data))
	}
	newMeta, _ := metayaml.Read(filepath.Join(global, "qr", metayaml.FileName))
	if newMeta.Lockfile.Hash == oldMeta.Lockfile.Hash {
		t.Fatal("hash must change after update")
	}
	if !newMeta.Lockfile.InstalledAt.Equal(oldInstalledAt) {
		t.Fatal("InstalledAt must be preserved on update")
	}
	if _, err := os.Stat(filepath.Join(global, "qr.futi-backup")); !os.IsNotExist(err) {
		t.Fatal("backup must be removed after update")
	}
}

func TestRunModulesUpdateAttachFailureWarns(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src, "--skip-attach"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Источник изменился: новая версия с failing attach.
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\nattach: \"exit 5\"\ncommands:\n  run:\n    cmd: echo v2\n")

	if err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, []string{"qr"}); err != nil {
		t.Fatalf("attach failure on update must be warning, not error: %v", err)
	}
	// Новое дерево сохранено (НЕ rollback).
	data, _ := os.ReadFile(filepath.Join(global, "qr", "config.yaml"))
	if !strings.Contains(string(data), "echo v2") {
		t.Fatal("new tree must survive attach failure (no rollback)")
	}
}

func TestRunModulesUpdateNoMetaError(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	installFixtureModule(t, global, "hand", nil) // без meta

	err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), newTestCache(), []string{"hand"})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected no-meta error, got %v", err)
	}
}

// Ребёнок (хук attach), убитый SIGINT, прерывает обновление всех модулей:
// следующие модули не трогаются.
func TestRunModulesUpdateAllStopsOnInterrupt(t *testing.T) {
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
	writeFile(t, filepath.Join(srcA, "config.yaml"), "name: a\nattach: \"kill -INT $$\"\ncommands:\n  run:\n    cmd: echo v2\n")
	writeFile(t, filepath.Join(srcB, "config.yaml"), "name: b\ncommands:\n  run:\n    cmd: echo v2\n")

	err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, nil)
	if err == nil {
		t.Fatal("ожидалась ошибка прерывания")
	}
	data, _ := os.ReadFile(filepath.Join(global, "b", "config.yaml"))
	if strings.Contains(string(data), "echo v2") {
		t.Fatal("модуль b не должен обновляться после прерывания a")
	}
}

// Дерево модуля уже заменено, когда attach прерван сигналом, — кэш обязан
// быть сброшен (иначе list/help покажут старое описание).
func TestRunModulesUpdateInterruptedAttachInvalidatesCache(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src, "--skip-attach"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\nattach: \"kill -INT $$\"\ncommands:\n  run:\n    cmd: echo v2\n")

	// Посторонняя запись в кэше — update должен её сбросить.
	stranger := makeSourceModule(t, t.TempDir(), "stranger", "")
	if _, err := cache.LoadModule(filepath.Join(stranger, "config.yaml"), stranger); err != nil {
		t.Fatal(err)
	}

	err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, []string{"qr"})
	if err == nil {
		t.Fatal("ожидалась ошибка прерывания")
	}
	if got := cache.ModuleCount(); got != 0 {
		t.Fatalf("кэш должен быть сброшен после прерванного attach, записей: %d", got)
	}
}

// При прерывании attach ошибка печатается один раз (возвращённой ошибкой в
// main), а не дублируется предупреждением и строкой цикла.
func TestRunModulesUpdateInterruptPrintsErrorOnce(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "a", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src, "--skip-attach"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	writeFile(t, filepath.Join(src, "config.yaml"), "name: a\nattach: \"kill -INT $$\"\ncommands:\n  run:\n    cmd: echo v2\n")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	updErr := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, nil)
	os.Stderr = origStderr
	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)

	if updErr == nil {
		t.Fatal("ожидалась ошибка прерывания")
	}
	if n := strings.Count(captured.String(), "signal: interrupt"); n != 0 {
		t.Fatalf("в stderr %d упоминаний сигнала, want 0 (ошибку печатает main):\n%s", n, captured.String())
	}
}

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
	attach := `trap "exit 0" TERM; echo ready > ` + ready + `; while :; do sleep 0.05; done`
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

// Собранный futi при прерывании attach печатает ошибку ровно один раз:
// предупреждение и строка цикла update её не дублируют.
func TestFutiUpdateInterruptPrintsErrorOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "futi")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	mods := filepath.Join(tmp, "mods")
	src := makeSourceModule(t, t.TempDir(), "a", "")
	env := append(os.Environ(),
		"FUTI_MODULES_DIR="+mods,
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = tmp
		cmd.Env = env
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stderr.Bytes(), err
	}
	if out, err := run("modules", "install", src, "--skip-attach"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(src, "config.yaml"),
		"name: a\nattach: \"kill -INT $$\"\ncommands:\n  run:\n    cmd: echo v2\n")

	stderr, err := run("modules", "update")
	if err == nil {
		t.Fatal("ожидалась ошибка прерывания")
	}
	t.Logf("stderr:\n%s", stderr)
	if n := strings.Count(string(stderr), "signal: interrupt"); n != 1 {
		t.Fatalf("в stderr %d вхождений signal: interrupt, want 1:\n%s", n, stderr)
	}
}
