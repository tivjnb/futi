package core_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulecache"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRT создаёт Runtime с локальным каталогом dir и пустым глобальным каталогом.
func newRT(t *testing.T, dir string) *core.Runtime {
	t.Helper()
	rt, err := core.NewRuntime(core.Options{LocalDir: dir, ModulesDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

type stubLoader struct {
	mod   *execmodule.Module
	calls int
}

func (s *stubLoader) LoadModule(string, string) (*execmodule.Module, error) {
	s.calls++
	return s.mod, nil
}

func TestRuntimeLocalSourceFutiYaml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: local\ncommands:\n  greet:\n    cmd: echo hi\n")

	src, err := newRT(t, dir).Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	manifest, root, err := src.Resolve("local")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if manifest != filepath.Join(dir, "futi.yaml") {
		t.Fatalf("manifest = %q", manifest)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
}

func TestRuntimeLocalSourceFutiDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".futi", "local.yaml"), "name: local\ncmd: echo\n")

	src, err := newRT(t, dir).Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	manifest, _, err := src.Resolve("local")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if manifest != filepath.Join(dir, ".futi", "local.yaml") {
		t.Fatalf("manifest = %q", manifest)
	}
}

func TestRuntimeLocalSourceNone(t *testing.T) {
	rt := newRT(t, t.TempDir())
	src, err := rt.Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if src != rt.GlobalSource() {
		t.Fatalf("Source() = %T, want global source", src)
	}
}

func TestRuntimeLocalSourceBothFutiYamlAndDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: yamlmod\ncmd: echo\n")
	writeFile(t, filepath.Join(dir, ".futi", "local.yaml"), "name: local\ncmd: echo\n")

	src, err := newRT(t, dir).Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	for _, ns := range []string{"yamlmod", "local"} {
		if _, _, err := src.Resolve(ns); err != nil {
			t.Fatalf("Resolve(%q): %v", ns, err)
		}
	}
}

// Loader из Options используется для загрузки futi.yaml (а не LoadModule напрямую).
func TestRuntimeLocalSourceUsesLoader(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: real\ncmd: echo\n")
	spy := &stubLoader{mod: &execmodule.Module{Name: "spy_name", Root: dir}}

	opts := core.Options{LocalDir: dir, ModulesDir: t.TempDir()}
	core.SetLoader(&opts, spy)
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	src, err := rt.Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if _, _, err := src.Resolve("spy_name"); err != nil {
		t.Fatalf("spy_name must resolve (loader used): %v", err)
	}
	if spy.calls != 1 {
		t.Fatalf("loader calls = %d, want 1", spy.calls)
	}
}

func TestRuntimeLocalSourceWithCache(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: cached\ncmd: echo\n")

	src, err := newRT(t, dir).Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if _, _, err := src.Resolve("cached"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestRuntimeLocalSourceFutiYamlName(t *testing.T) {
	for _, tt := range []struct{ name, body, ns string }{
		{"без name", "commands:\n  greet:\n    cmd: echo hi\n", "futi"},
		{"с name", "name: x\ncommands:\n  greet:\n    cmd: echo hi\n", "x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "futi.yaml"), tt.body)
			src, err := newRT(t, dir).Source()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := src.Resolve(tt.ns); err != nil {
				t.Fatalf("Resolve(%q): %v", tt.ns, err)
			}
		})
	}
}

func TestRuntimeLocalSourceInvalidFutiYamlReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: [\n  invalid")

	_, err := newRT(t, dir).Source()
	if err == nil || !strings.HasPrefix(err.Error(), "local source: ") {
		t.Fatalf("Source() err = %v, want prefix %q", err, "local source: ")
	}
}

func TestRuntimeSourceLayeredWhenLocalPresent(t *testing.T) {
	dir := t.TempDir()
	global := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: local\ncmd: echo\n")
	writeFile(t, filepath.Join(global, "glob.yaml"), "name: glob\ncmd: echo\n")

	rt, err := core.NewRuntime(core.Options{LocalDir: dir, ModulesDir: global})
	if err != nil {
		t.Fatal(err)
	}
	src, err := rt.Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if src == rt.GlobalSource() {
		t.Fatal("Source() must be layered, got the global source")
	}
	for _, ns := range []string{"local", "glob"} {
		if _, _, err := src.Resolve(ns); err != nil {
			t.Fatalf("Resolve(%q): %v", ns, err)
		}
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, body)
	return path
}

func TestRuntimeInvalidConfigReturnsError(t *testing.T) {
	path := writeConfig(t, "sources: [\n  invalid")
	_, err := core.NewRuntime(core.Options{ConfigPath: path})
	if err == nil || !strings.HasPrefix(err.Error(), "config: ") {
		t.Fatalf("NewRuntime err = %v, want prefix %q", err, "config: ")
	}
}

func TestRuntimeValidConfigWithSources(t *testing.T) {
	modDir := t.TempDir()
	path := writeConfig(t, fmt.Sprintf("sources:\n  - path: %s\n", modDir))

	rt, err := core.NewRuntime(core.Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if rt.GlobalSource() == nil {
		t.Fatal("GlobalSource() = nil")
	}
}

func TestRuntimeFallbackToModulesDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "m.yaml"), "name: m\ncmd: echo\n")
	rt, err := core.NewRuntime(core.Options{
		ConfigPath: filepath.Join(t.TempDir(), "missing.yaml"),
		ModulesDir: dir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if rt.Config() != nil {
		t.Fatal("Config() must be nil without config file")
	}
	if _, _, err := rt.GlobalSource().Resolve("m"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestRuntimeConfigReturned(t *testing.T) {
	modDir := t.TempDir()
	path := writeConfig(t, fmt.Sprintf("sources:\n  - path: %s\ncache:\n  refresh: force_only\n", modDir))

	rt, err := core.NewRuntime(core.Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	cfg := rt.Config()
	if cfg == nil {
		t.Fatal("Config() = nil")
	}
	if cfg.Cache.Refresh != "force_only" {
		t.Fatalf("Cache.Refresh = %q, want force_only", cfg.Cache.Refresh)
	}
}

// ~/… и относительный path в config.yaml не зависят от cwd.
func TestRuntimeExpandsTildeAndRelativePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cfgDir := filepath.Join(home, ".config", "futi")
	writeFile(t, filepath.Join(cfgDir, "modules", "a.yaml"), "name: a\ncommands:\n  run:\n    cmd: echo a\n")
	writeFile(t, filepath.Join(cfgDir, "extra", "b.yaml"), "name: b\ncommands:\n  run:\n    cmd: echo b\n")
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	writeFile(t, cfgPath, "sources:\n  - path: ~/.config/futi/modules\n  - path: extra\n")
	t.Chdir(t.TempDir())

	rt, err := core.NewRuntime(core.Options{ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	mods, err := rt.GlobalSource().List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, m := range mods {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "a,b" {
		t.Fatalf("modules = %q, want %q", got, "a,b")
	}
}

func TestRuntimeCachePathFromDefaultOptions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")
	opts, err := core.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(opts.CachePath, filepath.Join("futi", "modulecache.gob")) {
		t.Fatalf("CachePath = %q", opts.CachePath)
	}
}

func TestRuntimeCacheNilConfigUsesAlwaysPolicy(t *testing.T) {
	rt, err := core.NewRuntime(core.Options{ModulesDir: t.TempDir(), CachePath: filepath.Join(t.TempDir(), "c.gob")})
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.Cache().PolicyName(); got != "every_time" {
		t.Fatalf("PolicyName = %q, want every_time", got)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestRuntimeCacheForceOnly(t *testing.T) {
	path := writeConfig(t, "cache:\n  refresh: force_only\n")
	rt, err := core.NewRuntime(core.Options{
		ConfigPath: path,
		ModulesDir: t.TempDir(),
		CachePath:  filepath.Join(t.TempDir(), "c.gob"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.Cache().PolicyName(); got != "force_only" {
		t.Fatalf("PolicyName = %q, want force_only", got)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestRuntimeCachePathErrUsesMemory(t *testing.T) {
	var warns []string
	rt, err := core.NewRuntime(core.Options{
		ModulesDir:   t.TempDir(),
		CachePathErr: errors.New("boom"),
		Warn:         func(m string) { warns = append(warns, m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.Cache().PolicyName(); got != (modulecache.AlwaysPolicy{}).String() {
		t.Fatalf("PolicyName = %q", got)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(warns) != 1 || warns[0] != "cache: boom (using in-memory only)" {
		t.Fatalf("warnings = %q", warns)
	}
	if rt.CachePath() != "" {
		t.Fatalf("CachePath() = %q, want empty", rt.CachePath())
	}
}

func TestRuntimeInvalidPolicyWarnsOnce(t *testing.T) {
	path := writeConfig(t, "cache:\n  refresh: bogus\n")
	var warns []string
	rt, err := core.NewRuntime(core.Options{
		ConfigPath: path,
		ModulesDir: t.TempDir(),
		Warn:       func(m string) { warns = append(warns, m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	c1, c2 := rt.Cache(), rt.Cache()
	if c1 != c2 {
		t.Fatal("Cache() returned different pointers")
	}
	if len(warns) != 1 {
		t.Fatalf("warnings = %q, want exactly one", warns)
	}
	if !strings.HasPrefix(warns[0], "cache config: ") || !strings.HasSuffix(warns[0], " (using default every_time)") {
		t.Fatalf("warning = %q", warns[0])
	}
	if c1.PolicyName() != "every_time" {
		t.Fatalf("PolicyName = %q", c1.PolicyName())
	}
}

func TestRuntimeLazy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: [\n  invalid")
	cachePath := filepath.Join(t.TempDir(), "c.gob")

	rt, err := core.NewRuntime(core.Options{LocalDir: dir, ModulesDir: t.TempDir(), CachePath: cachePath})
	if err != nil {
		t.Fatalf("NewRuntime must not touch local files: %v", err)
	}
	_ = rt.Cache()
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("cache file must not exist before Start, stat err = %v", err)
	}
	_, err1 := rt.Source()
	if err1 == nil || !strings.HasPrefix(err1.Error(), "local source: ") {
		t.Fatalf("Source() err = %v", err1)
	}
	_, err2 := rt.Source()
	if err2 != err1 {
		t.Fatalf("second Source() err = %v, want same error %v", err2, err1)
	}
}

func TestRuntimeNoLocalDir(t *testing.T) {
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".futi", "local.yaml"), "name: local\ncmd: echo\n")
	t.Chdir(cwd)

	rt, err := core.NewRuntime(core.Options{LocalDir: "", ModulesDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	src, err := rt.Source()
	if err != nil {
		t.Fatal(err)
	}
	if src != rt.GlobalSource() {
		t.Fatalf("Source() = %T, want global source", src)
	}
}

func TestRuntimeLocalDirDot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".futi", "local.yaml"), "name: local\ncmd: echo\n")
	t.Chdir(dir)

	var got [2]string
	for i, ld := range []string{".", dir} {
		src, err := newRT(t, ld).Source()
		if err != nil {
			t.Fatal(err)
		}
		manifest, _, err := src.Resolve("local")
		if err != nil {
			t.Fatalf("LocalDir %q: %v", ld, err)
		}
		got[i] = manifest
	}
	if got[0] != got[1] || !filepath.IsAbs(got[0]) {
		t.Fatalf("manifests = %q, want equal absolute paths", got)
	}
}

func TestRuntimeConcurrentFirstUse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: local\ncmd: echo\n")
	rt := newRT(t, dir)

	const n = 8
	srcs := make([]discovery.Source, n)
	caches := make([]*modulecache.Cache, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s, err := rt.Source()
			if err != nil {
				t.Error(err)
			}
			srcs[i], caches[i] = s, rt.Cache()
		}()
	}
	close(start)
	wg.Wait()
	for i := 1; i < n; i++ {
		if srcs[i] != srcs[0] || caches[i] != caches[0] {
			t.Fatalf("goroutine %d got different source/cache", i)
		}
	}
}

func TestRuntimeEmptyModulesDirNoSources(t *testing.T) {
	_, err := core.NewRuntime(core.Options{ModulesDir: ""})
	if err == nil || err.Error() != "modules dir: not set" {
		t.Fatalf("NewRuntime err = %v, want %q", err, "modules dir: not set")
	}
}

func TestRuntimeStartPartialError(t *testing.T) {
	dirA := t.TempDir()
	dirB := filepath.Join(t.TempDir(), "missing")
	writeFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ncmd: echo x\n")
	path := writeConfig(t, fmt.Sprintf("sources:\n  - path: %s\n  - path: %s\n", dirA, dirB))

	rt, err := core.NewRuntime(core.Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	var pe *discovery.PartialError
	if err := rt.Start(); !errors.As(err, &pe) {
		t.Fatalf("Start() err = %v, want *discovery.PartialError", err)
	}
}
