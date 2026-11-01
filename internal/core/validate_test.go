package core_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulerunner"
)

func TestValidateInReportsAllErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.yaml"), `
name: bad
commands:
  empty: {}
  build:
    args:
      - name: tag
        type: float
      - name: tag
    cmd: "docker build {{.tag}}"
`)
	rep, err := core.ValidateIn(discovery.NewGlobalSource(dir), "bad")
	if err != nil {
		t.Fatalf("ValidateIn err = %v, want nil (schema errors are report data)", err)
	}
	joined := ""
	for _, e := range rep.Errors {
		joined += e.Error() + "\n"
	}
	if len(rep.Errors) != 3 || !strings.Contains(joined, "empty") || !strings.Contains(joined, "float") {
		t.Errorf("Errors = %q, want exactly 3 errors incl. 'empty' and 'float'", joined)
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

func TestValidateInParseErrorHasPrefix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "broken.yaml"), "name: [\n  invalid")
	_, err := core.ValidateIn(discovery.NewGlobalSource(dir), "broken")
	if err == nil || !strings.HasPrefix(err.Error(), "validate: ") {
		t.Fatalf("err = %v, want prefix %q", err, "validate: ")
	}
}

func TestRuntimeValidateUsesRuntimeSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: loc\ncmd: echo hi\n")
	rep, err := newRT(t, dir).Validate("loc")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("Errors = %v, want none for a valid local module", rep.Errors)
	}
}

func TestRuntimeValidatePropagatesSourceError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: [\n  invalid")
	_, err := newRT(t, dir).Validate("loc")
	if err == nil || !strings.HasPrefix(err.Error(), "local source: ") {
		t.Fatalf("err = %v, want prefix %q", err, "local source: ")
	}
}

func collisionSource(t *testing.T) (discovery.Source, string, string) {
	t.Helper()
	global, local := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(global, "dev.yaml"), `
name: dev
commands:
  deploy:
    cmd: "echo deploy"
`)
	writeFile(t, filepath.Join(local, "dev", "deploy.yaml"), `
name: deploy
cmd: "echo module"
`)
	return discovery.NewLayeredSource(discovery.NewGlobalSource(local), discovery.NewGlobalSource(global)), global, local
}

func TestValidateCollision(t *testing.T) {
	src, global, local := collisionSource(t)
	rep, err := core.ValidateIn(src, "dev")
	if err != nil {
		t.Fatalf("ValidateIn err = %v", err)
	}
	if len(rep.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly one collision", rep.Errors)
	}
	want := `path "dev deploy" is ambiguous: command "deploy" of module dev (` + filepath.Join(global, "dev.yaml") +
		`) and module dev.deploy (` + filepath.Join(local, "dev", "deploy.yaml") + `)`
	if got := rep.Errors[0].Error(); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	var ce *modulerunner.CollisionError
	if !errors.As(rep.Errors[0], &ce) {
		t.Errorf("error %T is not *modulerunner.CollisionError", rep.Errors[0])
	}
}

func TestValidateCollisionNamespaceDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev.yaml"), "name: dev\ncommands:\n  tools:\n    cmd: \"echo\"\n")
	writeFile(t, filepath.Join(dir, "dev", "tools", "a.yaml"), "name: a\ncmd: \"echo\"\n")
	rep, err := core.ValidateIn(discovery.NewGlobalSource(dir), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0].Error(), "and namespace dev.tools") {
		t.Errorf("Errors = %v, want one collision with namespace dev.tools", rep.Errors)
	}
}

func TestValidateNoCollisionWithoutOverlap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev.yaml"), "name: dev\ncommands:\n  build:\n    cmd: \"echo\"\n")
	writeFile(t, filepath.Join(dir, "dev", "deploy.yaml"), "name: deploy\ncmd: \"echo\"\n")
	rep, err := core.ValidateIn(discovery.NewGlobalSource(dir), "dev")
	if err != nil || len(rep.Errors) != 0 {
		t.Errorf("rep = %+v, err = %v, want no errors", rep, err)
	}
}

func TestValidateShadowWarningOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), `
name: db
args:
  - name: target
cmd: "echo {{.target}}"
commands:
  migrate:
    cmd: "echo migrate"
`)
	rep, err := core.ValidateIn(discovery.NewGlobalSource(dir), "db")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range rep.Warnings {
		if strings.Contains(w, "are subcommands") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("shadow warnings = %d in %q, want 1", n, rep.Warnings)
	}
}
