package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
)

// TestModulesValidate проверяет подкоманду `modules validate <ns>`.
func TestModulesValidate(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "good.yaml"), "name: good\ncommands:\n  ps:\n    cmd: \"echo ps\"\n")
	writeMainTestFile(t, filepath.Join(dir, "bad.yaml"), "name: bad\ncommands:\n  empty: {}\n")
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runModules(&buf, io.Discard, nil, rt, []string{"validate", "good"}); err != nil {
		t.Fatalf("good: %v", err)
	}
	if !strings.Contains(buf.String(), "OK") {
		t.Errorf("good: want OK, got %q", buf.String())
	}

	buf.Reset()
	err = runModules(&buf, io.Discard, nil, rt, []string{"validate", "bad"})
	if err == nil || !strings.Contains(buf.String(), "ERROR") {
		t.Errorf("bad: err=%v out=%q, want ERROR report and error", err, buf.String())
	}

	const usage = "usage: futi modules validate <namespace>"
	for _, args := range [][]string{
		{"validate"}, {"validate", "a", "b"}, {"validate", "-h"}, {"validate", "--help"}, {"validate", "good", "--x"},
	} {
		err := runModules(io.Discard, io.Discard, nil, rt, args)
		if err == nil || err.Error() != usage {
			t.Errorf("%v: err = %v, want %q", args, err, usage)
		}
	}

	if err := runModules(io.Discard, io.Discard, nil, rt, []string{"validate", "a/b"}); err == nil || !strings.Contains(err.Error(), "invalid module name") {
		t.Errorf("a/b: err = %v, want invalid module name", err)
	}
}

const validateHintManifest = `
name: m
commands:
  ps:
    cmd: "echo ps"
`

// TestOldValidateSyntaxHint: старый синтаксис не находит команду validate и
// получает подсказку про `modules validate`.
func TestOldValidateSyntaxHint(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"m", "validate"}, "hint: use 'futi modules validate m'"},
		{[]string{"validate"}, "hint: use 'futi modules validate <namespace>'"},
		{[]string{"nomod", "validate"}, "hint: use 'futi modules validate <namespace>'"},
		{[]string{"m", "ps", "validate"}, "hint: use 'futi modules validate <namespace>'"},
		{[]string{"m", "validate", "-h"}, "hint: use 'futi modules validate m'"},
		{[]string{"m", "validate", "help"}, "hint: use 'futi modules validate m'"},
		{[]string{"validate", "-h"}, "hint: use 'futi modules validate <namespace>'"},
	}
	for _, c := range cases {
		_, stderr, code := runFutiBin(t, validateHintManifest, c.args...)
		if code == 0 || !strings.Contains(stderr, c.want) {
			t.Errorf("%v: code=%d stderr=%q, want hint %q", c.args, code, stderr, c.want)
		}
	}
	// Обычная ошибка без validate подсказки не получает.
	_, stderr, _ := runFutiBin(t, validateHintManifest, "m", "nope")
	if strings.Contains(stderr, "hint:") {
		t.Errorf("m nope: unexpected hint in %q", stderr)
	}
}

// TestOldValidateSyntaxHintNamespaceDir: каталог-namespace не модуль, поэтому
// подсказка общая, без несуществующего модуля.
func TestOldValidateSyntaxHintNamespaceDir(t *testing.T) {
	mods := t.TempDir()
	writeFile(t, filepath.Join(mods, "dev", "x.yaml"), "name: x\ncmd: \"echo x\"\n")
	_, stderr, code := runFutiBinary(t, mods, "dev", "validate")
	want := "hint: use 'futi modules validate <namespace>'"
	if code == 0 || !strings.Contains(stderr, want) {
		t.Errorf("code=%d stderr=%q, want hint %q", code, stderr, want)
	}
}

// TestUserValidateCommandRuns: пользовательская команда validate исполняется,
// её ошибка исполнения подсказки не получает.
func TestUserValidateCommandRuns(t *testing.T) {
	stdout, _, code := runFutiBin(t, "name: m\ncommands:\n  validate:\n    cmd: \"echo user-validate\"\n", "m", "validate")
	if code != 0 || !strings.Contains(stdout, "user-validate") {
		t.Fatalf("code=%d stdout=%q, want user-validate", code, stdout)
	}
	_, stderr, code := runFutiBin(t, "name: m\ncommands:\n  validate:\n    cmd: \"false\"\n", "m", "validate")
	if code == 0 || strings.Contains(stderr, "hint:") {
		t.Fatalf("code=%d stderr=%q, want failure without hint", code, stderr)
	}
}

// TestRunValidateValidModule проверяет, что валидный модуль → "OK".
func TestRunValidateValidModule(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "docker.yaml")
	writeMainTestFile(t, manifestPath, `
name: docker
description: Docker management
commands:
  ps:
    cmd: "docker ps"
  build:
    params:
      - name: tag
        type: string
        required: true
    cmd: "docker build -t {{.tag}} ."
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "OK") {
		t.Fatalf("expected OK in output, got:\n%s", output)
	}
}

// TestRunValidateInvalidModule проверяет, что невалидный модуль → отчёт с ERROR.
func TestRunValidateInvalidModule(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bad.yaml")
	writeMainTestFile(t, manifestPath, `
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

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "bad")
	if err == nil {
		t.Fatal("expected error for invalid module, got nil")
	}

	output := buf.String()
	if !strings.Contains(output, "ERROR") {
		t.Fatalf("expected ERROR in output, got:\n%s", output)
	}
	// Должно показать несколько ошибок, а не только первую.
	if !strings.Contains(output, "empty") {
		t.Fatalf("expected 'empty' command error in output, got:\n%s", output)
	}
	if !strings.Contains(output, "float") {
		t.Fatalf("expected 'float' type error in output, got:\n%s", output)
	}
}

// TestRunValidateWithWarnings проверяет, что warnings показываются.
func TestRunValidateWithWarnings(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  migrate:
    commands:
      up:
        cmd: "echo up"
      down:
        cmd: "echo down"
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "db")
	// Warnings не блокируют — err должен быть nil.
	if err != nil {
		t.Fatalf("unexpected error (warnings should not block): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "WARNING") {
		t.Fatalf("expected WARNING in output, got:\n%s", output)
	}
}

// TestRunValidateModuleNotFound проверяет, что несуществующий namespace → ошибка.
func TestRunValidateModuleNotFound(t *testing.T) {
	dir := t.TempDir()
	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing module, got nil")
	}
}

// --- #39: -v/-r flags don't pollute params for regular commands ---
