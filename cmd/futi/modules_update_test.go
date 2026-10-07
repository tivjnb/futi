package main

import (
	"bytes"
	"errors"
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
	if b.String() != "qr: updated\n" {
		t.Fatalf("unexpected output: %q", b.String())
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
	t.Cleanup(func() { os.Stderr = origStderr })
	updErr := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, nil)
	os.Stderr = origStderr
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var captured bytes.Buffer
	if _, err := captured.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	r.Close()

	if updErr == nil {
		t.Fatal("ожидалась ошибка прерывания")
	}
	if n := strings.Count(captured.String(), "signal: interrupt"); n != 0 {
		t.Fatalf("в stderr %d упоминаний сигнала, want 0 (ошибку печатает main):\n%s", n, captured.String())
	}
}

// Собранный futi при прерывании attach печатает ошибку ровно один раз:
// предупреждение и строка цикла update её не дублируют.
func TestFutiUpdateInterruptPrintsErrorOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	tmp := t.TempDir()
	bin := builtFutiBinary(t)
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
		cmd.Stdout = io.Discard
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

// Собранный futi при прерывании attach в `modules update` гибнет от того же
// сигнала (а не выходит с кодом 1): иначе bash продолжит цикл
// `for m in a b c; do futi modules update $m; done` после Ctrl+C.
func TestFutiModulesUpdateDiesFromSignalWhenAttachInterrupted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	tmp := t.TempDir()
	bin := builtFutiBinary(t)
	mods := filepath.Join(tmp, "mods")
	src := makeSourceModule(t, t.TempDir(), "a", "")
	env := append(os.Environ(),
		"FUTI_MODULES_DIR="+mods,
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	run := func(args ...string) (*exec.Cmd, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = tmp
		cmd.Env = env
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		return cmd, cmd.Run()
	}
	if _, err := run("modules", "install", src, "--skip-attach"); err != nil {
		t.Fatalf("install: %v", err)
	}
	writeFile(t, filepath.Join(src, "config.yaml"),
		"name: a\nattach: \"kill -INT $$\"\ncommands:\n  run:\n    cmd: echo v2\n")

	cmd, err := run("modules", "update")
	if err == nil {
		t.Fatal("ожидалась ошибка прерывания")
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Fatalf("futi завершился %v, want гибель от SIGINT", cmd.ProcessState)
	}
}

// Обычная (не прерывающая) ошибка modules-команды по-прежнему даёт код 1.
func TestFutiModulesOrdinaryErrorExitsWithOne(t *testing.T) {
	tmp := t.TempDir()
	cmd := exec.Command(builtFutiBinary(t), "modules", "update", "no-such-module")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"FUTI_MODULES_DIR="+filepath.Join(tmp, "mods"),
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Fatalf("код выхода = %d, want 1", code)
	}
}

func TestRunModulesUpdateAllUpdatesEachDir(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	project := t.TempDir()
	t.Chdir(project)

	srcGlobal := makeSourceModule(t, t.TempDir(), "tool", "")
	srcLocal := makeSourceModule(t, t.TempDir(), "tool", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{srcGlobal}); err != nil {
		t.Fatalf("install global: %v", err)
	}
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{srcLocal, "--local"}); err != nil {
		t.Fatalf("install local: %v", err)
	}

	writeFile(t, filepath.Join(srcGlobal, "config.yaml"), "name: tool\ncommands:\n  run:\n    cmd: echo global-v2\n")
	writeFile(t, filepath.Join(srcLocal, "config.yaml"), "name: tool\ncommands:\n  run:\n    cmd: echo local-v2\n")

	var b bytes.Buffer
	if err := runModulesUpdate(&b, discovery.NewGlobalSource(global), cache, nil); err != nil {
		t.Fatalf("runModulesUpdate: %v", err)
	}

	for dir, want := range map[string]string{
		filepath.Join(global, "tool"):           "echo global-v2",
		filepath.Join(project, ".futi", "tool"): "echo local-v2",
	} {
		data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s: want %q, got:\n%s", dir, want, data)
		}
	}
	// Одноимённые модули различаются в выводе путём каталога.
	for _, dir := range []string{filepath.Join(global, "tool"), filepath.Join(project, ".futi", "tool")} {
		if !strings.Contains(b.String(), "tool ("+dir+"): updated") {
			t.Errorf("output must name %s: %s", dir, b.String())
		}
	}
}

// Ошибка одного из одноимённых модулей несёт подпись с путём; второй
// модуль всё равно обновляется.
func TestRunModulesUpdateAllSameNameErrorLabelled(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	project := t.TempDir()
	t.Chdir(project)

	srcGlobal := makeSourceModule(t, t.TempDir(), "tool", "")
	srcLocal := makeSourceModule(t, t.TempDir(), "tool", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{srcGlobal}); err != nil {
		t.Fatalf("install global: %v", err)
	}
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{srcLocal, "--local"}); err != nil {
		t.Fatalf("install local: %v", err)
	}
	if err := os.RemoveAll(srcGlobal); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(srcLocal, "config.yaml"), "name: tool\ncommands:\n  run:\n    cmd: echo local-v2\n")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })
	updErr := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, nil)
	os.Stderr = origStderr
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var captured bytes.Buffer
	if _, err := captured.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	r.Close()

	globalLabel := "tool (" + filepath.Join(global, "tool") + ")"
	if updErr == nil || !strings.Contains(updErr.Error(), "failed to update: "+globalLabel) {
		t.Fatalf("want failed to update with %q, got %v", globalLabel, updErr)
	}
	if !strings.Contains(captured.String(), "update "+globalLabel+":") {
		t.Errorf("stderr must contain labelled error: %s", captured.String())
	}
	data, _ := os.ReadFile(filepath.Join(project, ".futi", "tool", "config.yaml"))
	if !strings.Contains(string(data), "echo local-v2") {
		t.Errorf("local must be updated:\n%s", data)
	}
}

// Backup от прошлого сбоя может быть единственной рабочей копией: update
// отказывается и ничего не трогает; после ручного удаления проходит.
func TestRunModulesUpdateRefusesStaleBackup(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	stale := filepath.Join(global, "qr.futi-backup")
	writeFile(t, filepath.Join(stale, "junk.txt"), "junk\n")
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo v2\n")
	oldConfig, _ := os.ReadFile(filepath.Join(global, "qr", "config.yaml"))

	source := discovery.NewGlobalSource(global)
	err := runModulesUpdate(&bytes.Buffer{}, source, cache, []string{"qr"})
	if err == nil || !strings.Contains(err.Error(), stale) {
		t.Fatalf("want error naming %s, got %v", stale, err)
	}
	if _, err := os.Stat(filepath.Join(stale, "junk.txt")); err != nil {
		t.Fatalf("backup must be untouched: %v", err)
	}
	gotConfig, _ := os.ReadFile(filepath.Join(global, "qr", "config.yaml"))
	if string(gotConfig) != string(oldConfig) {
		t.Fatalf("moduleRoot must be untouched, got:\n%s", gotConfig)
	}

	if err := os.RemoveAll(stale); err != nil {
		t.Fatal(err)
	}
	if err := runModulesUpdate(&bytes.Buffer{}, source, cache, []string{"qr"}); err != nil {
		t.Fatalf("update after manual removal: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(global, "qr", "config.yaml"))
	if !strings.Contains(string(data), "echo v2") {
		t.Fatalf("new tree must be installed:\n%s", data)
	}
}

// Ошибка записи lockfile после переноса откатывает дерево: старое на месте.
func TestRunModulesUpdateMetaWriteFailureRollsBack(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	dest := filepath.Join(global, "qr")
	oldConfig, _ := os.ReadFile(filepath.Join(dest, "config.yaml"))
	oldMeta, err := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo v2\n")

	orig := writeMeta
	writeMeta = func(string, *metayaml.Meta) error { return errors.New("disk full") }
	t.Cleanup(func() { writeMeta = orig })

	err = runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, []string{"qr"})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("want disk full error, got %v", err)
	}
	gotConfig, _ := os.ReadFile(filepath.Join(dest, "config.yaml"))
	if string(gotConfig) != string(oldConfig) {
		t.Fatalf("old tree must be restored, got:\n%s", gotConfig)
	}
	gotMeta, err := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	if err != nil || gotMeta.Lockfile.Hash != oldMeta.Lockfile.Hash {
		t.Fatalf("old lockfile must stay: err=%v meta=%+v", err, gotMeta)
	}
	if _, err := os.Stat(dest + ".futi-backup"); !os.IsNotExist(err) {
		t.Fatalf("backup must not remain, stat err = %v", err)
	}
}

// captureStderr выполняет fn и возвращает всё, что она записала в os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })
	fn()
	os.Stderr = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	r.Close()
	return buf.String()
}

// Каталог *.futi-backup не модуль: update без имени не перезаписывает его и
// не запускает в нём attach, а предупреждает о нём в stderr.
func TestRunModulesUpdateAllLeavesBackupUntouched(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Состояние после kill между Rename и moveTree: каталога qr нет, есть backup.
	backup := filepath.Join(global, "qr.futi-backup")
	if err := os.Rename(filepath.Join(global, "qr"), backup); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(backup, "marker.txt"), "precious\n")
	oldConfig, _ := os.ReadFile(filepath.Join(backup, "config.yaml"))
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo v2\n")

	var out bytes.Buffer
	stderr := captureStderr(t, func() {
		if err := runModulesUpdate(&out, discovery.NewGlobalSource(global), cache, nil); err != nil {
			t.Errorf("runModulesUpdate: %v", err)
		}
	})

	if data, err := os.ReadFile(filepath.Join(backup, "marker.txt")); err != nil || string(data) != "precious\n" {
		t.Fatalf("marker must stay: err=%v data=%q", err, data)
	}
	gotConfig, _ := os.ReadFile(filepath.Join(backup, "config.yaml"))
	if string(gotConfig) != string(oldConfig) {
		t.Fatalf("backup config must not be overwritten, got:\n%s", gotConfig)
	}
	if !strings.Contains(stderr, backup) {
		t.Errorf("stderr must warn about %s, got: %q", backup, stderr)
	}
}

// update одного модуля, которого нет, но рядом лежит его backup: ошибка
// подсказывает про backup.
func TestRunModulesUpdateResolveFailureMentionsBackup(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	backup := filepath.Join(global, "qr.futi-backup")
	writeFile(t, filepath.Join(backup, "config.yaml"), "name: qr\n")

	err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), newTestCache(), []string{"qr"})
	if err == nil || !strings.Contains(err.Error(), backup) {
		t.Fatalf("want error mentioning %s, got %v", backup, err)
	}
}

// Backup проверяется до fetch: даже «up to date» не маскирует его наличие.
func TestRunModulesUpdateStaleBackupRefusedBeforeFetch(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	stale := filepath.Join(global, "qr.futi-backup")
	writeFile(t, filepath.Join(stale, "junk.txt"), "junk\n")
	if err := os.RemoveAll(src); err != nil { // fetch бы упал
		t.Fatal(err)
	}
	err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, []string{"qr"})
	if err == nil || !strings.Contains(err.Error(), "backup") || strings.Contains(err.Error(), "fetch") {
		t.Fatalf("want backup refusal before fetch, got %v", err)
	}
}

// Сбой удаления backup после успешного update не фатален, но о нём предупреждают.
func TestRunModulesUpdateRemoveBackupFailureWarns(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	writeFile(t, filepath.Join(src, "config.yaml"), "name: qr\ncommands:\n  run:\n    cmd: echo v2\n")

	orig := removeBackup
	removeBackup = func(string) error { return errors.New("busy") }
	t.Cleanup(func() { removeBackup = orig })

	var out bytes.Buffer
	stderr := captureStderr(t, func() {
		if err := runModulesUpdate(&out, discovery.NewGlobalSource(global), cache, []string{"qr"}); err != nil {
			t.Errorf("update must succeed: %v", err)
		}
	})
	want := "Warning: remove backup " + filepath.Join(global, "qr.futi-backup") + ": busy"
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr must contain %q, got %q", want, stderr)
	}
}
