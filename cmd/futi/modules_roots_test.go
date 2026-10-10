package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	t.Setenv("XDG_CONFIG_HOME", "")
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

	var b bytes.Buffer
	if err := runModulesList(&b, nil); err != nil {
		t.Fatalf("runModulesList: %v", err)
	}
	if !strings.Contains(b.String(), "dev.tools.qr") {
		t.Fatalf("list output lacks dev.tools.qr:\n%s", b.String())
	}
}

func TestModulesUninstallFromConfigSource(t *testing.T) {
	src := t.TempDir()
	installFixtureModule(t, src, "qr", &metayaml.Meta{})
	writeFutiConfig(t, sourceConfig(src, "", ""))
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := runModulesUninstall(&b, strings.NewReader(""), discovery.NewGlobalSource(src), newTestCache(), []string{"qr", "--yes"}); err != nil {
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

	roots, err := readModuleRoots()
	if err != nil {
		t.Fatalf("readModuleRoots: %v", err)
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

	roots, err := readModuleRoots()
	if err != nil {
		t.Fatalf("readModuleRoots: %v", err)
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

	roots, err := readModuleRoots()
	if err != nil {
		t.Fatalf("readModuleRoots: %v", err)
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
		err = runModulesInstall(&bytes.Buffer{}, nil, newTestCache(), []string{src})
	})
	if err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if !strings.Contains(stderr, global+" is not listed in config.yaml sources") {
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
	if err := runModulesInstall(&b, nil, newTestCache(), []string{src, "--namespace", "images"}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
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

	var err error
	stderr := captureStderr(t, func() {
		err = runModulesInit(&bytes.Buffer{}, []string{"tools/qr", "--global"})
	})
	if err != nil {
		t.Fatalf("runModulesInit: %v", err)
	}
	if !strings.Contains(stderr, global+" is not listed in config.yaml sources") {
		t.Fatalf("stderr lacks sources warning:\n%s", stderr)
	}
}

func TestModulesInitGlobalReportsPrefixedName(t *testing.T) {
	global := t.TempDir()
	writeFutiConfig(t, sourceConfig(global, "dev", ""))
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := runModulesInit(&b, []string{"tools/qr", "--global"}); err != nil {
		t.Fatalf("runModulesInit: %v", err)
	}
	if !strings.Contains(b.String(), "Module is available as dev.tools.qr") {
		t.Fatalf("output lacks prefixed name:\n%s", b.String())
	}
}

func TestModulesRootsInvalidConfig(t *testing.T) {
	writeFutiConfig(t, "sources: [\n")
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	err := runModulesList(&bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "modules list: config:") {
		t.Fatalf("err = %v, want modules list: config: error", err)
	}
}

// Модуль, доступный из двух корней (./.futi/qr — симлинк на пакет в
// глобальном каталоге), обходится один раз: update не обновит его дважды.
func TestScanRootsDedupsModuleAcrossRoots(t *testing.T) {
	skipSymlinksOnWindows(t)
	global := t.TempDir()
	installFixtureModule(t, global, "qr", &metayaml.Meta{})
	local := t.TempDir()
	if err := os.Symlink(filepath.Join(global, "qr"), filepath.Join(local, "qr")); err != nil {
		t.Fatal(err)
	}

	entries := scanInstalled([]moduleRoot{{Dir: global}, {Dir: local}})
	if len(entries) != 1 || entries[0].Dir != filepath.Join(global, "qr") {
		t.Fatalf("entries = %+v, want one qr in %s", entries, global)
	}
}
