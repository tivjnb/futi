package modulerunner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"futi/internal/discovery"
	"futi/internal/execmodule"
	"futi/internal/modulecache"
)

// stubLoader — тестовый загрузчик модулей, который возвращает
// предзаготовленный модуль для любого пути и считает вызовы.
// Используется для проверки, что Run/ListCommands действительно
// обращаются к loader, а не к execmodule.LoadModule напрямую.
type stubLoader struct {
	mod   *execmodule.Module
	calls int
}

func (s *stubLoader) LoadModule(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	s.calls++
	return s.mod, nil
}

func TestRunExecutesCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "greet.sh")
	script := []byte("#!/bin/sh\nprintf 'hello %s\\n' \"$1\"\n")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
commands:
  greet:
    params:
      - name: name
        type: string
        required: true
    cmd: "./greet.sh {{.name}}"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "testmod", "greet", map[string]string{"name": "world"}, "", nil, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "hello world\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "hello world\n")
	}
}

func TestRunModuleNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "nonexistent", "cmd", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}

func TestRunCommandNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
commands:
  exist:
    cmd: "echo hi"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "testmod", "nonexistent", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "nonexistent") || !strings.Contains(err.Error(), "testmod") {
		t.Fatalf("error should mention command and module names, got: %v", err)
	}
}

func TestRunMissingRequiredParamReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
commands:
  build:
    params:
      - name: tag
        type: string
        required: true
    cmd: "echo {{.tag}}"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "testmod", "build", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
}

func TestListCommandsReturnsCommandsWithDescriptions(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
description: Test module
commands:
  greet:
    description: Greet someone
    cmd: "echo hi"
  build:
    description: Build artifact
    cmd: "echo build"
  plain:
    cmd: "echo plain"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)
	cmds, err := ListCommands(source, "testmod", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(cmds))
	}

	byName := make(map[string]string)
	for _, c := range cmds {
		byName[c.Name] = c.Description
	}
	if byName["greet"] != "Greet someone" {
		t.Fatalf("unexpected greet description: %q", byName["greet"])
	}
	if byName["build"] != "Build artifact" {
		t.Fatalf("unexpected build description: %q", byName["build"])
	}
	if byName["plain"] != "" {
		t.Fatalf("expected empty description for plain, got %q", byName["plain"])
	}
}

func TestListCommandsModuleNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	source := discovery.NewGlobalSource(dir)
	_, err := ListCommands(source, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}

func TestRunPreservesExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()

	manifestPath := filepath.Join(dir, "testmod.yaml")
	manifest := []byte(`
name: testmod
commands:
  fail:
    cmd: "sh -c 'exit 42'"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "testmod", "fail", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError in chain, got: %T: %v", err, err)
	}
	if exitErr.ExitCode() != 42 {
		t.Fatalf("expected exit code 42, got %d", exitErr.ExitCode())
	}
}

func TestRunSubcommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "migrate.sh")
	script := []byte("#!/bin/sh\nprintf 'migrating %s\\n' \"$1\"\n")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	manifestPath := filepath.Join(dir, "db.yaml")
	manifest := []byte(`
name: db
commands:
  migrate:
    commands:
      up:
        cmd: "./migrate.sh up"
      down:
        cmd: "./migrate.sh down"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "db.migrate", "up", map[string]string{}, "", nil, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "migrating up\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "migrating up\n")
	}
}

func TestRunNestedSubcommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "migrate.sh")
	script := []byte("#!/bin/sh\nprintf 'migrating %s %s\\n' \"$1\" \"$2\"\n")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	manifestPath := filepath.Join(dir, "db.yaml")
	manifest := []byte(`
name: db
commands:
  migrate:
    commands:
      up:
        commands:
          force:
            cmd: "./migrate.sh up force"
          safe:
            cmd: "./migrate.sh up safe"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "db.migrate.up", "force", map[string]string{}, "", nil, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "migrating up force\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "migrating up force\n")
	}
}

func TestRunSubcommandWithParams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "migrate.sh")
	script := []byte("#!/bin/sh\nprintf 'creating %s\\n' \"$1\"\n")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	manifestPath := filepath.Join(dir, "db.yaml")
	manifest := []byte(`
name: db
commands:
  migrate:
    commands:
      create:
        params:
          - name: name
            type: string
            required: true
        cmd: "./migrate.sh {{.name}}"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "db.migrate", "create", map[string]string{"name": "initial"}, "", nil, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "creating initial\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "creating initial\n")
	}
}

func TestRunCommandWithSubcommandsNoSubcommandSpecified(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	manifest := []byte(`
name: db
commands:
  migrate:
    commands:
      up:
        cmd: "echo up"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "db", "migrate", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error: 'migrate' requires a subcommand")
	}
}

func TestRunDefaultCmdWithArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	manifest := []byte(`
name: echo_mod
cmd: "echo {{.args}}"
commands:
  upper:
    cmd: "echo {{.args}} | tr a-z A-Z"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "echo_mod", "", map[string]string{}, "hello world", []string{"hello", "world"}, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "hello world\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "hello world\n")
	}
}

func TestRunCommandWithArgsViaDoubleDash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	manifest := []byte(`
name: echo_mod
commands:
  echo:
    cmd: "echo {{.args}}"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "echo_mod", "echo", map[string]string{}, "hello world", []string{"hello", "world"}, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "hello world\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "hello world\n")
	}
}

func TestRunCommandWithArgsList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	manifest := []byte(`
name: echo_mod
commands:
  first:
    cmd: "echo {{index .args_list 0}}"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "echo_mod", "first", map[string]string{}, "hello world", []string{"hello", "world"}, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "hello\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "hello\n")
	}
}

func TestRunNoDefaultCmdReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	manifest := []byte(`
name: db
commands:
  status:
    cmd: "echo status"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "db", "", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error: module has no default command")
	}
}

func TestRunCmdAndCommandsOrthogonal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	manifest := []byte(`
name: db
commands:
  migrate:
    cmd: "echo migrate default"
    commands:
      up:
        cmd: "echo migrate up"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	// migrate без subcommand → выполняется migrate.cmd (fallback)
	var stdout1 bytes.Buffer
	err := Run(context.Background(), source, "db", "migrate", map[string]string{}, "", nil, &stdout1, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout1.String(); got != "migrate default\n" {
		t.Fatalf("migrate fallback: unexpected output: %q, want %q", got, "migrate default\n")
	}

	// migrate up → выполняется up.cmd
	var stdout2 bytes.Buffer
	err = Run(context.Background(), source, "db.migrate", "up", map[string]string{}, "", nil, &stdout2, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout2.String(); got != "migrate up\n" {
		t.Fatalf("migrate up: unexpected output: %q, want %q", got, "migrate up\n")
	}
}

// TestRunUsesLoader доказывает, что Run обращается к loader для получения
// модуля, а не к execmodule.LoadModule напрямую. Stub-загрузчик возвращает
// модуль с cmd "echo SPY", тогда как файл на диске содержит cmd "echo REAL".
// Если вывод содержит SPY — loader используется.
func TestRunUsesLoader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	// Реальный файл с командой "echo REAL" — используется только для source.Resolve.
	manifestPath := filepath.Join(dir, "stub.yaml")
	if err := os.WriteFile(manifestPath, []byte(`
name: stub
commands:
  go:
    cmd: "echo REAL"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	source := discovery.NewGlobalSource(dir)

	// Stub-загрузчик возвращает модуль с cmd "echo SPY".
	spy := &stubLoader{
		mod: &execmodule.Module{
			Name: "stub",
			Root: dir,
			Commands: map[string]*execmodule.Command{
				"go": {Cmd: "echo SPY"},
			},
		},
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "stub", "go", map[string]string{}, "", nil, &stdout, nil, spy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "SPY\n" {
		t.Fatalf("expected SPY output (loader used), got %q", got)
	}
	if spy.calls != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", spy.calls)
	}
}

// TestRunWithNilLoaderBackwardsCompat проверяет, что nil-loader
// сохраняет старое поведение: Run грузит модуль через execmodule.LoadModule.
func TestRunWithNilLoaderBackwardsCompat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	if err := os.WriteFile(manifestPath, []byte(`
name: echo_mod
cmd: "echo {{.args}}"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "echo_mod", "", map[string]string{}, "works", []string{"works"}, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "works\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "works\n")
	}
}

// TestRunWithCacheLoader проверяет интеграцию с *modulecache.Cache:
// Run принимает кэш как loader и корректно выполняет команду.
func TestRunWithCacheLoader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	if err := os.WriteFile(manifestPath, []byte(`
name: echo_mod
cmd: "echo {{.args}}"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	source := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "echo_mod", "", map[string]string{}, "cached", []string{"cached"}, &stdout, nil, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "cached\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "cached\n")
	}
}

// TestListCommandsUsesLoader доказывает, что ListCommands обращается к loader.
func TestListCommandsUsesLoader(t *testing.T) {
	dir := t.TempDir()
	// Файл с командой "real" — используется только для source.Resolve.
	manifestPath := filepath.Join(dir, "stub.yaml")
	if err := os.WriteFile(manifestPath, []byte(`
name: stub
commands:
  real:
    cmd: "echo real"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	source := discovery.NewGlobalSource(dir)

	spy := &stubLoader{
		mod: &execmodule.Module{
			Name: "stub",
			Commands: map[string]*execmodule.Command{
				"spy":   {Description: "from spy"},
				"extra": {Description: "extra from spy"},
			},
		},
	}

	cmds, err := ListCommands(source, "stub", spy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands from spy, got %d", len(cmds))
	}
	byName := make(map[string]string)
	for _, c := range cmds {
		byName[c.Name] = c.Description
	}
	if byName["spy"] != "from spy" {
		t.Fatalf("expected spy command from loader, got %q", byName["spy"])
	}
	if spy.calls != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", spy.calls)
	}
}
