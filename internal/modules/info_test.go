package modules

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/metayaml"
)

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

func TestRunModulesPath(t *testing.T) {
	root := installFixtureModule(t, t.TempDir(), "qr", nil)
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	got, err := newTestManager(t, io.Discard, nil).Path("qr")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(root, "qr"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestManagerInfoUnknownName(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	info, err := newTestManager(t, io.Discard, nil).Info("nope")
	if err == nil || !strings.HasPrefix(err.Error(), "modules info: ") {
		t.Fatalf("Info err = %v, want modules info: prefix", err)
	}
	if info.Root != "" || info.Meta != nil {
		t.Fatalf("Info = %+v, want empty on resolve error", info)
	}
}

func TestManagerInfoBrokenSidecar(t *testing.T) {
	root := installFixtureModule(t, t.TempDir(), "qr", nil)
	if err := os.WriteFile(filepath.Join(root, "qr", metayaml.FileName), []byte("lockfile: [broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	info, err := newTestManager(t, io.Discard, nil).Info("qr")
	if err == nil || !strings.HasPrefix(err.Error(), "modules info: ") {
		t.Fatalf("Info err = %v, want modules info: prefix", err)
	}
	if want := filepath.Join(root, "qr"); info.Root != want || info.Meta != nil {
		t.Fatalf("Info = %+v, want Root %q and nil Meta", info, want)
	}
}
