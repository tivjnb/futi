package core_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
)

func TestValidateInReportsAllErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.yaml"), `
name: bad
commands:
  empty: {}
  build:
    params:
      - name: tag
        type: float
      - name: tag
        type: string
    cmd: "docker build"
`)
	rep, err := core.ValidateIn(discovery.NewGlobalSource(dir), "bad")
	if err != nil {
		t.Fatalf("ValidateIn err = %v, want nil (schema errors are report data)", err)
	}
	joined := ""
	for _, e := range rep.Errors {
		joined += e.Error() + "\n"
	}
	if len(rep.Errors) < 2 || !strings.Contains(joined, "empty") || !strings.Contains(joined, "float") {
		t.Errorf("Errors = %q, want at least the 'empty' and 'float' errors", joined)
	}
}

func TestValidateInReportsWarnings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), `
name: db
commands:
  migrate:
    commands:
      up:
        cmd: "echo up"
      down:
        cmd: "echo down"
`)
	rep, err := core.ValidateIn(discovery.NewGlobalSource(dir), "db")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) != 0 || len(rep.Warnings) == 0 {
		t.Errorf("Errors = %v, Warnings = %v, want no errors and some warnings", rep.Errors, rep.Warnings)
	}
}

func TestValidateInNotFound(t *testing.T) {
	_, err := core.ValidateIn(discovery.NewGlobalSource(t.TempDir()), "nonexistent")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !strings.HasPrefix(err.Error(), "validate: ") {
		t.Errorf("err = %q, want prefix %q", err, "validate: ")
	}
}

func TestValidateInInvalidNamespace(t *testing.T) {
	_, err := core.ValidateIn(discovery.NewGlobalSource(t.TempDir()), "a..b")
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}
