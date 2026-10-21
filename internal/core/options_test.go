package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultModulesDirFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", dir)

	got, err := DefaultModulesDir()
	if err != nil {
		t.Fatalf("DefaultModulesDir() err = %v", err)
	}
	if got != dir {
		t.Fatalf("DefaultModulesDir() = %q, want %q", got, dir)
	}
}

func TestDefaultModulesDirDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", "")
	t.Setenv("HOME", home)

	got, err := DefaultModulesDir()
	if err != nil {
		t.Fatalf("DefaultModulesDir() err = %v", err)
	}
	want := filepath.Join(home, ".config", "futi", "modules")
	if got != want {
		t.Fatalf("DefaultModulesDir() = %q, want %q", got, want)
	}
}

func TestLoadDefaultConfigMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := LoadDefaultConfig()
	if err != nil || cfg != nil {
		t.Fatalf("LoadDefaultConfig() = (%v, %v), want (nil, nil)", cfg, err)
	}
}

func TestLoadDefaultConfigInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "futi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("sources: ["), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadDefaultConfig(); err == nil {
		t.Fatal("LoadDefaultConfig() err = nil, want error for invalid yaml")
	}
}

func TestDefaultOptions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("путь кэша зависит от XDG_CACHE_HOME (Linux)")
	}
	home, cache, mods, work := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("FUTI_MODULES_DIR", mods)
	t.Chdir(work)

	opts, err := DefaultOptions()
	if err != nil {
		t.Fatalf("DefaultOptions() err = %v", err)
	}
	if want := filepath.Join(home, ".config", "futi", "config.yaml"); opts.ConfigPath != want {
		t.Errorf("ConfigPath = %q, want %q", opts.ConfigPath, want)
	}
	if opts.ModulesDir != mods {
		t.Errorf("ModulesDir = %q, want %q", opts.ModulesDir, mods)
	}
	if want := filepath.Join(cache, "futi", "modulecache.gob"); opts.CachePath != want {
		t.Errorf("CachePath = %q, want %q", opts.CachePath, want)
	}
	if opts.CachePathErr != nil {
		t.Errorf("CachePathErr = %v, want nil", opts.CachePathErr)
	}
	gotLocal, err := filepath.EvalSymlinks(opts.LocalDir)
	if err != nil {
		t.Fatal(err)
	}
	wantLocal, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	if gotLocal != wantLocal {
		t.Errorf("LocalDir = %q, want %q", gotLocal, wantLocal)
	}
	if opts.Warn != nil {
		t.Error("Warn != nil, want nil")
	}
}

func TestDefaultOptionsNoHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("поведение пустого HOME проверяется на Linux")
	}
	t.Setenv("HOME", "")
	t.Setenv("FUTI_MODULES_DIR", "")

	_, err := DefaultOptions()
	if err == nil {
		t.Fatal("DefaultOptions() err = nil, want error")
	}
	if !strings.HasPrefix(err.Error(), "config path: ") {
		t.Fatalf("err = %q, want prefix %q", err, "config path: ")
	}
}

func TestDefaultOptionsCachePathErr(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("поведение XDG_CACHE_HOME проверяется на Linux")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "rel/cache")
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())

	opts, err := DefaultOptions()
	if err != nil {
		t.Fatalf("DefaultOptions() err = %v, want nil", err)
	}
	if opts.CachePath != "" {
		t.Errorf("CachePath = %q, want empty", opts.CachePath)
	}
	if opts.CachePathErr == nil || !strings.HasPrefix(opts.CachePathErr.Error(), "cache path: get cache dir: ") {
		t.Fatalf("CachePathErr = %v, want prefix %q", opts.CachePathErr, "cache path: get cache dir: ")
	}
}
