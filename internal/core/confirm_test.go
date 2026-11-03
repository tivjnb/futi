package core_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/execmodule"
)

const confirmModule = `name: m
commands:
  db:
    commands:
      drop:
        destructive: true
        flags:
          - {name: marker, required: true}
          - {name: mode, choices: [soft, hard]}
        cmd: [sh, -c, 'touch "$1"', sh, "{{.marker}}", "{{.mode}}"]
  safe:
    flags:
      - {name: marker, required: true}
    cmd: [sh, -c, 'touch "$1"', sh, "{{.marker}}"]
`

func newConfirmRT(t *testing.T) (*core.Runtime, string) {
	t.Helper()
	skipWindows(t)
	dir := t.TempDir()
	manifest := filepath.Join(dir, "m.yaml")
	writeFile(t, manifest, confirmModule)
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return rt, manifest
}

func TestExecuteConfirm(t *testing.T) {
	errBoom := errors.New("boom")
	fail := func(core.ConfirmRequest) (bool, error) {
		t.Fatal("Confirm must not be called")
		return false, nil
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	run := func(t *testing.T, words []string, in map[string]any, confirm func(core.ConfirmRequest) (bool, error), dry bool) (core.ExecuteResult, error, string) {
		rt, _ := newConfirmRT(t)
		tgt, _, err := rt.ResolveWords(execmodule.FrontendCLI, words)
		if err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(t.TempDir(), "marker")
		in["marker"] = marker
		res, err := rt.Execute(context.Background(), tgt, core.ExecuteRequest{Inputs: in, Confirm: confirm, DryRun: dry})
		return res, err, marker
	}
	drop := []string{"m", "db", "drop"}

	t.Run("nil callback requires confirmation", func(t *testing.T) {
		res, err, marker := run(t, drop, map[string]any{}, nil, false)
		if !errors.Is(err, core.ErrConfirmationRequired) {
			t.Fatalf("err = %v, want ErrConfirmationRequired", err)
		}
		if exists(marker) || len(res.Argv) == 0 {
			t.Errorf("marker exists = %v, argv = %q", exists(marker), res.Argv)
		}
	})
	t.Run("declined", func(t *testing.T) {
		var got core.ConfirmRequest
		res, err, marker := run(t, drop, map[string]any{}, func(r core.ConfirmRequest) (bool, error) { got = r; return false, nil }, false)
		if !errors.Is(err, core.ErrNotConfirmed) {
			t.Fatalf("err = %v, want ErrNotConfirmed", err)
		}
		if exists(marker) {
			t.Error("command ran")
		}
		if !reflect.DeepEqual(got.Path, []string{"m", "db", "drop"}) {
			t.Errorf("Path = %q", got.Path)
		}
		if !reflect.DeepEqual(got.Argv, res.Argv) || len(got.Argv) == 0 {
			t.Errorf("Argv = %q, res.Argv = %q", got.Argv, res.Argv)
		}
		if got.Module == "" || got.Module != res.Module {
			t.Errorf("Module = %q, res.Module = %q", got.Module, res.Module)
		}
	})
	t.Run("dotted path", func(t *testing.T) {
		var got core.ConfirmRequest
		run(t, []string{"m", "db.drop"}, map[string]any{}, func(r core.ConfirmRequest) (bool, error) { got = r; return false, nil }, false)
		if !reflect.DeepEqual(got.Path, []string{"m", "db", "drop"}) {
			t.Errorf("Path = %q, want [m db drop]", got.Path)
		}
	})
	t.Run("callback error", func(t *testing.T) {
		_, err, marker := run(t, drop, map[string]any{}, func(core.ConfirmRequest) (bool, error) { return false, errBoom }, false)
		if !errors.Is(err, errBoom) || exists(marker) {
			t.Errorf("err = %v, marker = %v", err, exists(marker))
		}
	})
	t.Run("confirmed", func(t *testing.T) {
		_, err, marker := run(t, drop, map[string]any{}, func(core.ConfirmRequest) (bool, error) { return true, nil }, false)
		if err != nil || !exists(marker) {
			t.Errorf("err = %v, marker = %v", err, exists(marker))
		}
	})
	t.Run("dry run skips confirmation", func(t *testing.T) {
		_, err, marker := run(t, drop, map[string]any{}, fail, true)
		if err != nil || exists(marker) {
			t.Errorf("err = %v, marker = %v", err, exists(marker))
		}
	})
	t.Run("invalid input skips confirmation", func(t *testing.T) {
		_, err, _ := run(t, drop, map[string]any{"mode": "bogus"}, fail, false)
		if !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("bad choice: err = %v", err)
		}
		rt, _ := newConfirmRT(t)
		tgt, _, _ := rt.ResolveWords(execmodule.FrontendCLI, drop)
		_, err = rt.Execute(context.Background(), tgt, core.ExecuteRequest{Confirm: fail})
		if !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("missing flag: err = %v", err)
		}
	})
	t.Run("non-destructive runs", func(t *testing.T) {
		_, err, marker := run(t, []string{"m", "safe"}, map[string]any{}, fail, false)
		if err != nil || !exists(marker) {
			t.Errorf("err = %v, marker = %v", err, exists(marker))
		}
	})
}
