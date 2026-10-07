package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/modulecache"
)

// makeSourceModule создаёт каталог модуля-источника в temp-каталоге.
func makeSourceModule(t *testing.T, parent, name, extra string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\ncommands:\n  run:\n    cmd: echo run\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newTestCache() *modulecache.Cache {
	return modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
}

func TestRunModulesInstallLocalDir(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")

	var b bytes.Buffer
	if err := runModulesInstall(&b, newTestCache(), []string{src}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}

	dest := filepath.Join(global, "qr")
	if _, err := os.Stat(filepath.Join(dest, "config.yaml")); err != nil {
		t.Fatalf("module not installed: %v", err)
	}
	meta, err := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	if err != nil {
		t.Fatalf("meta not written: %v", err)
	}
	if meta.Lockfile.Source != src || meta.Lockfile.Type != "local" {
		t.Fatalf("unexpected lockfile: %+v", meta.Lockfile)
	}
	if meta.Lockfile.Hash == "" {
		t.Fatal("expected tree hash in lockfile")
	}
}

func TestRunModulesInstallSingleYaml(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	srcDir := t.TempDir()
	yamlPath := filepath.Join(srcDir, "qr.yaml")
	os.WriteFile(yamlPath, []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644)

	var b bytes.Buffer
	if err := runModulesInstall(&b, newTestCache(), []string{yamlPath}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "qr", "config.yaml")); err != nil {
		t.Fatalf("single yaml must be wrapped into package: %v", err)
	}
}

func TestRunModulesInstallExistingError(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
}

func TestRunModulesInstallAttachRuns(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "attach: \"touch attach_marker\"\n")

	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "qr", "attach_marker")); err != nil {
		t.Fatalf("attach hook did not run: %v", err)
	}
}

func TestRunModulesInstallAttachFailureRollsBack(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "attach: \"exit 3\"\n")

	err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src})
	if err == nil {
		t.Fatal("expected attach failure error")
	}
	if _, statErr := os.Stat(filepath.Join(global, "qr")); !os.IsNotExist(statErr) {
		t.Fatal("destination must be removed after attach failure (rollback)")
	}
}

func TestRunModulesInstallSkipAttach(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "attach: \"touch attach_marker\"\n")

	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src, "--skip-attach"}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "qr", "attach_marker")); !os.IsNotExist(err) {
		t.Fatal("attach must be skipped")
	}
}

func TestRunModulesInstallNamespace(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")

	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src, "--namespace", "images"}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "images", "qr", "config.yaml")); err != nil {
		t.Fatalf("namespaced install failed: %v", err)
	}
}

func TestRunModulesInstallLocalFlag(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	cwd := t.TempDir()
	t.Chdir(cwd)

	src := makeSourceModule(t, t.TempDir(), "qr", "")

	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src, "--local"}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".futi", "qr", "config.yaml")); err != nil {
		t.Fatalf("local install failed: %v", err)
	}
}

func TestRunModulesInstallNotAModule(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	empty := t.TempDir() // каталог без config.yaml
	err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{empty})
	if err == nil || !strings.Contains(err.Error(), "not a futi module") {
		t.Fatalf("expected not-a-module error, got %v", err)
	}
}

func TestRunModulesInstallInvalidatesCache(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	cache := newTestCache()
	// Кладём в кэш постороннюю запись — install должен её сбросить.
	stranger := makeSourceModule(t, t.TempDir(), "stranger", "")
	if _, err := cache.LoadModule(filepath.Join(stranger, "config.yaml"), stranger); err != nil {
		t.Fatal(err)
	}

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if got := cache.ModuleCount(); got != 0 {
		t.Fatalf("cache must be invalidated after install, got %d entries", got)
	}
}

func TestRunModulesInstallLocalSourceAbsInLockfile(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	cwd := t.TempDir()
	makeSourceModule(t, cwd, "mod", "")
	t.Chdir(cwd)

	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{"./mod"}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	meta, err := metayaml.Read(filepath.Join(os.Getenv("FUTI_MODULES_DIR"), "mod", metayaml.FileName))
	if err != nil {
		t.Fatalf("meta not written: %v", err)
	}
	want := filepath.Join(cwd, "mod")
	if meta.Lockfile.Source != want {
		t.Fatalf("lockfile source: got %q, want %q", meta.Lockfile.Source, want)
	}
}

func TestRunModulesInstallLocalFlagHookAbsRoot(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	cwd := t.TempDir()
	t.Chdir(cwd)

	attach := "attach: \"test -d {{.module_root}} && echo {{.module_root}} $FUTI_MODULE_ROOT > $FUTI_MODULE_ROOT/roots\"\n"
	src := makeSourceModule(t, t.TempDir(), "qr", attach)

	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src, "--local"}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	dest := filepath.Join(cwd, ".futi", "qr")
	data, err := os.ReadFile(filepath.Join(dest, "roots"))
	if err != nil {
		t.Fatalf("hook did not write roots: %v", err)
	}
	want := dest + " " + dest
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("hook roots: got %q, want %q", got, want)
	}
}

// F56: одиночный q.yaml без name: получает имя q (а не q.yaml).
func TestRunModulesInstallYamlSourceName(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	yamlPath := filepath.Join(t.TempDir(), "q.yaml")
	if err := os.WriteFile(yamlPath, []byte("commands:\n  hi:\n    cmd: echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{yamlPath}); err != nil {
		t.Fatalf("runModulesInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "q", "config.yaml")); err != nil {
		t.Fatalf("expected root/q/config.yaml: %v", err)
	}
	if _, _, err := discovery.NewGlobalSource(global).Resolve("q"); err != nil {
		t.Fatalf("Resolve(q): %v", err)
	}
}

// F56: точка в имени модуля — разделитель namespace, такой модуль не резолвится.
func TestRunModulesInstallDottedNameRejected(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "my.tool", "")
	err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src})
	if err == nil || !strings.Contains(err.Error(), "invalid module name") {
		t.Fatalf("expected invalid module name error, got %v", err)
	}
	entries, _ := os.ReadDir(global)
	if len(entries) != 0 {
		t.Fatalf("root must stay empty, got %v", entries)
	}
}

// F59: установка рядом с одноимённым x.yaml создала бы конфликт discovery.
func TestRunModulesInstallConflictsWithYaml(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	if err := os.WriteFile(filepath.Join(global, "x.yaml"), []byte("commands:\n  a:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := makeSourceModule(t, t.TempDir(), "x", "")
	err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "x")); err == nil {
		t.Fatal("root/x must not be created")
	}
}

// F56: имя из fallbackName с точкой (foo-1.2) отвергается с подсказкой про name:.
func TestRunModulesInstallDottedFallbackNameHint(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	dir := filepath.Join(t.TempDir(), "foo-1.2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("commands:\n  run:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{dir})
	if err == nil || !strings.Contains(err.Error(), "name:") {
		t.Fatalf("expected error with name: hint, got %v", err)
	}
}

// F57: зарезервированное слово в namespace или в имени модуля отвергается
// до создания чего-либо в каталоге модулей.
func TestRunModulesInstallReservedSegments(t *testing.T) {
	cases := []struct {
		name string
		args func(src string) []string
		mod  string
	}{
		{"namespace cache", func(src string) []string { return []string{src, "--namespace", "cache"} }, "qr"},
		{"namespace a.list", func(src string) []string { return []string{src, "--namespace", "a.list"} }, "qr"},
		{"manifest name completion", func(src string) []string { return []string{src} }, "completion"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			global := t.TempDir()
			t.Setenv("FUTI_MODULES_DIR", global)
			t.Chdir(t.TempDir())
			src := makeSourceModule(t, t.TempDir(), tc.mod, "")
			err := runModulesInstall(&bytes.Buffer{}, newTestCache(), tc.args(src))
			if err == nil || !strings.Contains(err.Error(), "is a reserved name") {
				t.Fatalf("expected reserved name error, got %v", err)
			}
			entries, _ := os.ReadDir(global)
			if len(entries) != 0 {
				t.Fatalf("modules dir must stay empty, got %v", entries)
			}
		})
	}
}

func TestRunModulesInstallNonReservedNamespaceOK(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())
	src := makeSourceModule(t, t.TempDir(), "qr", "")
	if err := runModulesInstall(&bytes.Buffer{}, newTestCache(), []string{src, "--namespace", "validate.help"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "validate", "help", "qr", "config.yaml")); err != nil {
		t.Fatalf("module not installed: %v", err)
	}
}
