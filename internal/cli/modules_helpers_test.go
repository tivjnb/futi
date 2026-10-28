package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/metayaml"
)

// Copies of the internal/modules test helpers needed by the CLI tests.

// newTestRuntime builds a Runtime from the test environment (FUTI_MODULES_DIR,
// HOME/XDG_CONFIG_HOME, cwd) as the CLI does, but with an in-memory cache.
func newTestRuntime(t *testing.T) *core.Runtime {
	t.Helper()
	opts, err := core.DefaultOptions()
	if err != nil {
		t.Fatalf("DefaultOptions: %v", err)
	}
	opts.CachePath = ""
	opts.CachePathErr = nil
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

// installFixtureModule создаёт в dir пакет модуля name с config.yaml и
// (опционально) __futi_meta.yaml. Возвращает корень source.
func installFixtureModule(t *testing.T, dir, name string, meta *metayaml.Meta) string {
	t.Helper()
	moduleDir := filepath.Join(dir, name)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\ncommands:\n  run:\n    cmd: echo run\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		if err := metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), meta); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

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
