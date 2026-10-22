package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
)

func newCacheRuntime(t *testing.T, modulesDir, cachePath string) *core.Runtime {
	t.Helper()
	rt, err := core.NewRuntime(core.Options{ModulesDir: modulesDir, CachePath: cachePath})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestRuntimeCacheRefreshThenStatus(t *testing.T) {
	modulesDir := t.TempDir()
	writeFile(t, filepath.Join(modulesDir, "app.yaml"), "name: app\ncmd: echo app\n")
	cachePath := filepath.Join(t.TempDir(), "cache.gob")

	res, err := newCacheRuntime(t, modulesDir, cachePath).CacheRefresh()
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}

	st := newCacheRuntime(t, modulesDir, cachePath).CacheStatus()
	if st.Modules != 1 {
		t.Errorf("Modules = %d, want 1", st.Modules)
	}
	if st.LastRefresh.IsZero() {
		t.Errorf("LastRefresh is zero, want set")
	}
	if st.Path != cachePath {
		t.Errorf("Path = %q, want %q", st.Path, cachePath)
	}
	if st.Policy != "every_time" {
		t.Errorf("Policy = %q, want every_time", st.Policy)
	}
}

func TestRuntimeCacheStatusInMemory(t *testing.T) {
	st := newCacheRuntime(t, t.TempDir(), "").CacheStatus()
	if st.Path != "" {
		t.Errorf("Path = %q, want empty", st.Path)
	}
}

func TestRuntimeCacheClear(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cache.gob")
	writeFile(t, cachePath, "x")
	rt := newCacheRuntime(t, t.TempDir(), cachePath)
	if err := rt.CacheClear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("cache file still exists: %v", err)
	}
	if err := rt.CacheClear(); err != nil {
		t.Errorf("second clear: %v", err)
	}
	if err := newCacheRuntime(t, t.TempDir(), "").CacheClear(); err != nil {
		t.Errorf("in-memory clear: %v", err)
	}
}

func TestClearCacheError(t *testing.T) {
	// A non-empty directory cannot be removed by os.Remove.
	dir := filepath.Join(t.TempDir(), "d")
	writeFile(t, filepath.Join(dir, "f"), "x")
	err := core.ClearCache(dir)
	if err == nil || !strings.HasPrefix(err.Error(), "cache clear: remove "+dir+": ") {
		t.Fatalf("err = %v, want cache clear: remove prefix", err)
	}
}

func TestRefreshCachePartial(t *testing.T) {
	dirA := t.TempDir()
	dirB := filepath.Join(t.TempDir(), "missing")
	writeFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ncmd: echo x\n")
	cfg := &config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}}
	c := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	res, err := core.RefreshCache(c, discovery.NewCompositeSource(cfg))
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *discovery.PartialError", err)
	}
	if !strings.HasPrefix(err.Error(), "cache refresh: ") {
		t.Errorf("err text = %q, want cache refresh: prefix", err)
	}
	for _, class := range []error{core.ErrNotFound, core.ErrInvalidInput} {
		if errors.Is(err, class) {
			t.Errorf("partial error must not carry class %v: %v", class, err)
		}
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1", res.Total)
	}
}
