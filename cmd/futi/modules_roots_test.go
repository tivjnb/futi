package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/metayaml"
)

// writeFutiConfig переносит HOME во временный каталог и пишет туда
// ~/.config/futi/config.yaml с телом body (config.DefaultConfigPath).
func writeFutiConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".config", "futi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sourceConfig — config.yaml с одним source.
func sourceConfig(path, mountpoint, ns string) string {
	body := "sources:\n  - path: " + filepath.ToSlash(path) + "\n"
	if mountpoint != "" {
		body += "    mountpoint: " + mountpoint + "\n"
	}
	if ns != "" {
		body += "    mount_as_namespace: " + ns + "\n"
	}
	return body
}

func skipSymlinksOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges on Windows")
	}
}

func TestModulesListShowsConfigSources(t *testing.T) {
	src := t.TempDir()
	installFixtureModule(t, src, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/qr"}})
	writeFutiConfig(t, sourceConfig(src, "dev", "tools"))
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	entries, err := newTestManager(t, io.Discard, nil).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "dev.tools.qr" {
		t.Fatalf("List = %+v, want one dev.tools.qr", entries)
	}
}

func TestModulesUninstallFromConfigSource(t *testing.T) {
	src := t.TempDir()
	installFixtureModule(t, src, "qr", &metayaml.Meta{})
	writeFutiConfig(t, sourceConfig(src, "", ""))
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := newTestManager(t, &b, strings.NewReader("")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true}); err != nil {
		t.Fatalf("runModulesUninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "qr")); !os.IsNotExist(err) {
		t.Fatalf("module in source must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source root must survive: %v", err)
	}
}

// Корень source сам по себе — не цель удаления, хотя он среди корней.
func TestModulesUninstallRefusesConfigSourceRoot(t *testing.T) {
	src := t.TempDir()
	installFixtureModule(t, src, "qr", &metayaml.Meta{})
	writeFutiConfig(t, sourceConfig(src, "dev", ""))
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	roots, err := newTestManager(t, io.Discard, nil).moduleRoots()
	if err != nil {
		t.Fatalf("moduleRoots: %v", err)
	}
	dirs := rootDirs(roots)
	if err := ensureWithinRoot(src, dirs); err == nil {
		t.Fatalf("source root %s must be refused, roots %v", src, dirs)
	}
	if err := ensureWithinRoot(filepath.Join(src, "qr"), dirs); err != nil {
		t.Fatalf("module inside source must be allowed: %v", err)
	}
}

func TestModulesUpdateAllIncludesConfigSources(t *testing.T) {
	src := t.TempDir()
	installFixtureModule(t, src, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Type: "bogus"}})
	writeFutiConfig(t, sourceConfig(src, "dev", "tools"))
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	var err error
	stderr := captureStderr(t, func() {
		err = runModulesUpdate(&bytes.Buffer{}, strings.NewReader(""), discovery.NewGlobalSource(src), newTestCache(), nil)
	})
	// Неизвестный тип источника — ошибка именно этого модуля: значит, update до него дошёл.
	if err == nil || !strings.Contains(err.Error(), "dev.tools.qr") {
		t.Fatalf("err = %v, want failure of dev.tools.qr; stderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "update dev.tools.qr: unknown source type") {
		t.Fatalf("stderr lacks update of dev.tools.qr:\n%s", stderr)
	}
}

// backupHint ищет backup в корне, чей префикс совпадает с началом имени.
func TestBackupHintStripsRootPrefix(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "qr"+backupSuffix), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFutiConfig(t, sourceConfig(src, "dev", "tools"))
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	if got := backupHint("dev.tools.qr"); !strings.Contains(got, filepath.Join(src, "qr"+backupSuffix)) {
		t.Fatalf("backupHint(dev.tools.qr) = %q, want path of %s", got, filepath.Join(src, "qr"+backupSuffix))
	}
	if got := backupHint("qr"); got != "" {
		t.Fatalf("backupHint(qr) = %q, want empty (no root without prefix holds it)", got)
	}
}

func TestModulesListFollowsSymlinks(t *testing.T) {
	skipSymlinksOnWindows(t)
	writeFutiConfig(t, "")
	t.Chdir(t.TempDir())

	realRoot := t.TempDir()
	installFixtureModule(t, realRoot, "qr", &metayaml.Meta{})
	pkgs := t.TempDir()
	installFixtureModule(t, pkgs, "linked", &metayaml.Meta{})
	if err := os.Symlink(filepath.Join(pkgs, "linked"), filepath.Join(realRoot, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, filepath.Join(realRoot, "loop")); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(t.TempDir(), "modules")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUTI_MODULES_DIR", linkRoot)

	roots, err := newTestManager(t, io.Discard, nil).moduleRoots()
	if err != nil {
		t.Fatalf("moduleRoots: %v", err)
	}
	entries := scanInstalled(roots)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "alias,qr" {
		t.Fatalf("names = %v, want [alias qr]", names)
	}
}

// Один реальный каталог в двух корнях (source и симлинк на него в
// FUTI_MODULES_DIR) даёт одну запись — с префиксом source.
func TestModulesListDedupsRootsByRealPath(t *testing.T) {
	skipSymlinksOnWindows(t)
	src := t.TempDir()
	installFixtureModule(t, src, "qr", &metayaml.Meta{})
	writeFutiConfig(t, sourceConfig(src, "dev", ""))
	linkRoot := filepath.Join(t.TempDir(), "modules")
	if err := os.Symlink(src, linkRoot); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUTI_MODULES_DIR", linkRoot)
	t.Chdir(t.TempDir())

	roots, err := newTestManager(t, io.Discard, nil).moduleRoots()
	if err != nil {
		t.Fatalf("moduleRoots: %v", err)
	}
	entries := scanInstalled(roots)
	if len(entries) != 1 || entries[0].Name != "dev.qr" {
		t.Fatalf("entries = %+v, want one dev.qr", entries)
	}
}

func TestModulesInstallWarnsRootNotInSources(t *testing.T) {
	writeFutiConfig(t, sourceConfig(t.TempDir(), "", ""))
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())
	src := makeSourceModule(t, t.TempDir(), "qr", "")

	var err error
	stderr := captureStderr(t, func() {
		err = newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: src})
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(stderr, global+" is not listed in config.yaml sources; add it there to use, update or uninstall the module by name") {
		t.Fatalf("stderr lacks sources warning:\n%s", stderr)
	}
}

func TestModulesInstallReportsPrefixedName(t *testing.T) {
	global := t.TempDir()
	writeFutiConfig(t, sourceConfig(global, "dev", ""))
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())
	src := makeSourceModule(t, t.TempDir(), "qr", "")

	var b bytes.Buffer
	if err := newTestManager(t, &b, nil).Install(context.Background(), InstallOptions{Source: src, Namespace: "images"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(b.String(), "Module is available as dev.images.qr") {
		t.Fatalf("output lacks prefixed name:\n%s", b.String())
	}
}

func TestModulesInitGlobalWarnsRootNotInSources(t *testing.T) {
	writeFutiConfig(t, sourceConfig(t.TempDir(), "", ""))
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	var stderr bytes.Buffer
	if err := Init(&bytes.Buffer{}, &stderr, InitOptions{Path: "tools/qr", Global: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !strings.Contains(stderr.String(), global+" is not listed in config.yaml sources; add it there to use, update or uninstall the module by name") {
		t.Fatalf("stderr lacks sources warning:\n%s", stderr.String())
	}
}

func TestModulesInitGlobalReportsPrefixedName(t *testing.T) {
	global := t.TempDir()
	writeFutiConfig(t, sourceConfig(global, "dev", ""))
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := Init(&b, io.Discard, InitOptions{Path: "tools/qr", Global: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !strings.Contains(b.String(), "Module is available as dev.tools.qr") {
		t.Fatalf("output lacks prefixed name:\n%s", b.String())
	}
}

// Модуль, доступный из двух корней (./.futi/qr — симлинк на пакет в
// глобальном каталоге), обходится один раз и под путём без симлинка, даже
// если корень с симлинком обходится первым.
func TestScanRootsDedupsModuleAcrossRoots(t *testing.T) {
	skipSymlinksOnWindows(t)
	global := t.TempDir()
	installFixtureModule(t, global, "qr", &metayaml.Meta{})
	local := t.TempDir()
	if err := os.Symlink(filepath.Join(global, "qr"), filepath.Join(local, "qr")); err != nil {
		t.Fatal(err)
	}

	entries := scanInstalled([]moduleRoot{{Dir: local}, {Dir: global}})
	if len(entries) != 1 || entries[0].Dir != filepath.Join(global, "qr") {
		t.Fatalf("entries = %+v, want one qr in %s", entries, global)
	}
}

// В одном корне alias -> qr: list показывает qr (путь без симлинка), а
// update без имени обновляет qr и не трогает alias — он остаётся симлинком.
func TestModulesSymlinkAliasPrefersRealPath(t *testing.T) {
	skipSymlinksOnWindows(t)
	writeFutiConfig(t, "")
	global := t.TempDir()
	installFixtureModule(t, global, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/qr", Type: "bogus"}})
	alias := filepath.Join(global, "alias")
	if err := os.Symlink(filepath.Join(global, "qr"), alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	listed, err := newTestManager(t, io.Discard, nil).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "qr" {
		t.Fatalf("list must show qr and not alias, got %+v", listed)
	}

	stderr := captureStderr(t, func() {
		err = runModulesUpdate(&bytes.Buffer{}, strings.NewReader(""), discovery.NewGlobalSource(global), newTestCache(), nil)
	})
	if err == nil || err.Error() != "failed to update: qr" {
		t.Fatalf("err = %v, want failure of qr only; stderr:\n%s", err, stderr)
	}
	if info, lerr := os.Lstat(alias); lerr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("alias must stay a symlink, lstat = %v, %v", info, lerr)
	}
}

// Модуль, видимый только через симлинк-каталог, list показывает, а update без
// имени пропускает: замена дерева превратила бы симлинк в копию.
func TestModulesUpdateAllSkipsSymlinkedModules(t *testing.T) {
	skipSymlinksOnWindows(t)
	writeFutiConfig(t, "")
	global := t.TempDir()
	pkgs := t.TempDir()
	installFixtureModule(t, pkgs, "linked", &metayaml.Meta{Lockfile: metayaml.Lockfile{Type: "bogus"}})
	alias := filepath.Join(global, "alias")
	if err := os.Symlink(filepath.Join(pkgs, "linked"), alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	var out bytes.Buffer
	var err error
	stderr := captureStderr(t, func() {
		err = runModulesUpdate(&out, strings.NewReader(""), discovery.NewGlobalSource(global), newTestCache(), nil)
	})
	if err != nil {
		t.Fatalf("update must skip alias, err = %v; stderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "Warning: skipped alias: module directory is a symlink") {
		t.Fatalf("stderr must report the skipped module:\n%s", stderr)
	}
	if info, lerr := os.Lstat(alias); lerr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("alias must stay a symlink, lstat = %v, %v", info, lerr)
	}
}

// Симлинк-каталог namespace (dotfiles-схема global/images -> ~/dotfiles/images):
// сам модуль — обычный каталог, update без имени его обновляет.
func TestModulesUpdateAllUpdatesModuleInsideSymlinkedNamespace(t *testing.T) {
	skipSymlinksOnWindows(t)
	writeFutiConfig(t, "")
	global := t.TempDir()
	dotfiles := t.TempDir()
	installFixtureModule(t, filepath.Join(dotfiles, "images"), "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Type: "bogus"}})
	if err := os.Symlink(filepath.Join(dotfiles, "images"), filepath.Join(global, "images")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	var err error
	stderr := captureStderr(t, func() {
		err = runModulesUpdate(&bytes.Buffer{}, strings.NewReader(""), discovery.NewGlobalSource(global), newTestCache(), nil)
	})
	// Неизвестный тип источника — update дошёл до updateAt, а не пропустил модуль.
	if err == nil || err.Error() != "failed to update: images.qr" {
		t.Fatalf("err = %v, want failure of images.qr; stderr:\n%s", err, stderr)
	}
}

func TestModulesUpdateByNameThroughCompositeSource(t *testing.T) {
	src := t.TempDir()
	installFixtureModule(t, src, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Type: "bogus"}})
	writeFutiConfig(t, sourceConfig(src, "dev", "tools"))
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	cfg, err := core.LoadDefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	err = runModulesUpdate(&bytes.Buffer{}, strings.NewReader(""), discovery.NewCompositeSource(cfg), newTestCache(), []string{"dev.tools.qr"})
	// Неизвестный тип источника — значит, Resolve нашёл модуль и update дошёл до lockfile.
	if err == nil || !strings.Contains(err.Error(), `unknown source type "bogus"`) {
		t.Fatalf("err = %v, want unknown source type from updateAt", err)
	}
}

// Глобальный каталог строго внутри source: модуль виден с относительным путём.
func TestModulesInstallReportsNameWhenRootInsideSource(t *testing.T) {
	src := t.TempDir()
	global := filepath.Join(src, "modules")
	writeFutiConfig(t, sourceConfig(src, "dev", ""))
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())
	pkg := makeSourceModule(t, t.TempDir(), "qr", "")

	var b bytes.Buffer
	var err error
	stderr := captureStderr(t, func() {
		err = newTestManager(t, &b, nil).Install(context.Background(), InstallOptions{Source: pkg})
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if strings.Contains(stderr, "is not listed") {
		t.Fatalf("unexpected sources warning:\n%s", stderr)
	}
	if !strings.Contains(b.String(), "Module is available as dev.modules.qr") {
		t.Fatalf("output lacks dev.modules.qr:\n%s", b.String())
	}
}

func TestModulesInstallReportsNameWhenRootInsideUnprefixedSource(t *testing.T) {
	src := t.TempDir()
	global := filepath.Join(src, "modules")
	writeFutiConfig(t, sourceConfig(src, "", ""))
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())
	pkg := makeSourceModule(t, t.TempDir(), "qr", "")

	var b bytes.Buffer
	if err := newTestManager(t, &b, nil).Install(context.Background(), InstallOptions{Source: pkg}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(b.String(), "Module is available as modules.qr\n") {
		t.Fatalf("output lacks modules.qr:\n%s", b.String())
	}
}

// Корень, вложенный в другой корень, — не цель удаления: source /x и
// глобальный каталог /x/y с config.yaml; uninstall y удалил бы корень.
func TestModulesUninstallRefusesNestedRoot(t *testing.T) {
	src := t.TempDir()
	installFixtureModule(t, src, "y", &metayaml.Meta{})
	global := filepath.Join(src, "y")
	writeFutiConfig(t, sourceConfig(src, "", ""))
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("")).Uninstall(context.Background(), UninstallOptions{Name: "y", Yes: true})
	if err == nil {
		t.Fatal("uninstall of a modules root must be refused")
	}
	if _, statErr := os.Stat(filepath.Join(global, "config.yaml")); statErr != nil {
		t.Fatalf("root %s must survive: %v", global, statErr)
	}
}

func TestModuleRootsFromSkipsLocalWithoutLocalDir(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".futi"), 0o755); err != nil {
		t.Fatal(err)
	}
	global := t.TempDir()

	roots := moduleRootsFrom(nil, global, "")
	if len(roots) != 1 || roots[0].Dir != global {
		t.Fatalf("moduleRootsFrom(nil, global, \"\") = %+v, want only %s", roots, global)
	}

	roots = moduleRootsFrom(nil, global, project)
	want := filepath.Join(project, ".futi")
	if len(roots) != 2 || roots[0].Dir != global || roots[1].Dir != want {
		t.Fatalf("moduleRootsFrom(nil, global, project) = %+v, want %s then %s", roots, global, want)
	}
}
