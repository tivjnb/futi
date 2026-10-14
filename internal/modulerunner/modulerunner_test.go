package modulerunner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulecache"
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
    cmd: "{{quote .module_root}}/greet.sh {{.name}}"
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
        cmd: "{{quote .module_root}}/migrate.sh up"
      down:
        cmd: "{{quote .module_root}}/migrate.sh down"
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
            cmd: "{{quote .module_root}}/migrate.sh up force"
          safe:
            cmd: "{{quote .module_root}}/migrate.sh up safe"
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
        cmd: "{{quote .module_root}}/migrate.sh {{quote .name}}"
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

// TestLoadModuleExported проверяет, что экспортированная LoadModule
// делегирует в loader и fallback-ит на execmodule.LoadModule при nil
// (фикс #35 — дублирование с main.go устранено).
func TestLoadModuleExported(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "mod.yaml")
	if err := os.WriteFile(manifestPath, []byte(`name: mod
cmd: "echo hi"`), 0644); err != nil {
		t.Fatal(err)
	}

	// nil loader → fallback на execmodule.LoadModule
	mod, err := LoadModule(nil, manifestPath, dir)
	if err != nil {
		t.Fatalf("LoadModule with nil loader: %v", err)
	}
	if mod.Name != "mod" {
		t.Fatalf("expected name=mod, got %q", mod.Name)
	}

	// stub loader → делегирует
	stub := &stubLoader{mod: &execmodule.Module{Name: "stubbed"}}
	mod2, err := LoadModule(stub, manifestPath, dir)
	if err != nil {
		t.Fatalf("LoadModule with stub: %v", err)
	}
	if mod2.Name != "stubbed" {
		t.Fatalf("expected name=stubbed, got %q", mod2.Name)
	}
	if stub.calls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.calls)
	}
}

func TestRunPositionalMinEnforced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "pos.yaml")
	manifest := []byte(`
name: pos
commands:
  greet:
    cmd: "echo {{index .args_list 0}}"
    positional:
      min: 1
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	// Достаточно аргументов → выполняется.
	var stdout bytes.Buffer
	err := Run(context.Background(), source, "pos", "greet", map[string]string{}, "hello", []string{"hello"}, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "hello\n" {
		t.Fatalf("unexpected output: %q", got)
	}

	// Мало аргументов → ошибка про required positional.
	err = Run(context.Background(), source, "pos", "greet", map[string]string{}, "", []string{}, &stdout, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing positional arg")
	}
	if !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("expected 'at least 1' in error, got: %v", err)
	}
}

func TestRunDefaultCmdParams(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "meta.yaml")
	manifest := []byte(`
name: meta
cmd: "echo {{.sep}}{{if .upper}} UP{{end}} {{.args}}"
params:
  - name: sep
    type: string
    default: "|"
  - name: upper
    type: bool
    default: "false"
positional:
  min: 1
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "meta", "", map[string]string{}, "hello", []string{"hello"}, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "| hello\n" {
		t.Fatalf("unexpected output: %q, want %q", got, "| hello\n")
	}

	stdout.Reset()
	err = Run(context.Background(), source, "meta", "", map[string]string{"upper": "true"}, "hello", []string{"hello"}, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "| UP hello\n" {
		t.Fatalf("unexpected output with bool flag: %q, want %q", got, "| UP hello\n")
	}
}

func TestRunDefaultCmdUnknownParamRejected(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "meta.yaml")
	manifest := []byte(`
name: meta
cmd: "echo {{.args}}"
params:
  - name: json
    type: bool
    default: "false"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "meta", "", map[string]string{"bad": "1"}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error: unknown param on default cmd")
	}
	if !strings.Contains(err.Error(), "unknown param") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunDefaultCmdPositionalMin(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "meta.yaml")
	manifest := []byte(`
name: meta
cmd: "echo {{.args}}"
positional:
  min: 1
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	err := Run(context.Background(), source, "meta", "", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error: default cmd requires positional args")
	}
	if !strings.Contains(err.Error(), "at least 1 positional") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunRespectsParentCwd проверяет WORKDIR.md §2.2/§3.1: дочерний процесс
// исполняется с WorkDir = cwd родительского процесса (наследование, без
// WithDir(moduleRoot)). t.Chdir меняет cwd процесса — Run должен подхватить
// его для дочернего процесса.
func TestRunRespectsParentCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pwd не POSIX-совместимо для проверки на Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "probe.yaml")
	manifest := []byte(`
name: probe
cmd: "pwd"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// t.Chdir меняет cwd процесса — Run должен подхватить его как WorkDir
	// дочернего процесса (а не moduleRoot).
	t.Chdir(dir)

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "probe", "", map[string]string{}, "", nil, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimRight(stdout.String(), "\n"))
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	if got != want {
		t.Fatalf("cwd = %q, want %q (must inherit parent cwd, not moduleRoot)", got, want)
	}
}

// TestRunExposesModuleRootEnv проверяет, что FUTI_MODULE_ROOT пробрасывается
// в дочерний процесс (WORKDIR.md §2.3) — замена FUTI_CWD для скриптов,
// которым нужен путь к файлам модуля.
func TestRunExposesModuleRootEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("printenv not available on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "probe.yaml")
	manifest := []byte(`
name: probe
cmd: "printenv FUTI_MODULE_ROOT"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "probe", "", map[string]string{}, "", nil, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimRight(stdout.String(), "\n"))
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	if got != want {
		t.Fatalf("FUTI_MODULE_ROOT = %q, want %q", got, want)
	}
}

// TestRunNoFutiCwdEnv проверяет, что FUTI_CWD удалён из env (WORKDIR.md §2.3,
// чистый breaking change). Скрипты, которым нужен cwd вызова, используют
// getcwd() (cwd = cwd вызова futi, см. TestRunRespectsParentCwd).
func TestRunNoFutiCwdEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("printenv not available on Windows")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "probe.yaml")
	manifest := []byte(`
name: probe
cmd: "sh -c 'if [ -n \"$FUTI_CWD\" ]; then echo HAS_FUTI_CWD; else echo NO_FUTI_CWD; fi'"
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "probe", "", map[string]string{}, "", nil, &stdout, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "NO_FUTI_CWD" {
		t.Fatalf("env FUTI_CWD = %q, want absent (WORKDIR.md §2.3)", got)
	}
}

// Корневой config.yaml не должен становиться модулем для пустого/неизвестного namespace.
func TestRunRootConfigNotUsedAsModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("name: root\ncmd: echo ROOTCFG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := discovery.NewGlobalSource(dir)

	var stdout bytes.Buffer
	err := Run(context.Background(), source, "zzz", "x", map[string]string{}, "", nil, &stdout, nil, nil)
	if err == nil {
		t.Fatal("ожидалась ошибка для неизвестного модуля")
	}
	if !strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), `in module ""`) {
		t.Errorf("Run(zzz, x) err = %q, ожидалось not found без `in module \"\"`", err)
	}

	stdout.Reset()
	err = Run(context.Background(), source, "", "", map[string]string{}, "", nil, &stdout, nil, nil)
	if err == nil {
		t.Errorf("Run(\"\", \"\") без ошибки, stdout=%q", stdout.String())
	}
}

func TestRunConflictInSubnamespaceReturnsError(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.yaml", "name: a\ncommands:\n  b:\n    cmd: \"touch "+marker+"\"\n")
	write("a/b.yaml", "name: b\n")
	write("a/b/config.yaml", "name: b\n")

	source := discovery.NewGlobalSource(dir)
	err := Run(context.Background(), source, "a", "b", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error %q does not mention conflict", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("command a.yaml:b was executed despite conflict")
	}
}

func TestRunStatErrorDoesNotFallBack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root обходит права доступа")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.yaml", "name: a\ncommands:\n  b:\n    cmd: \"touch "+marker+"\"\n")
	write("a/b.yaml", "name: b\n")
	if err := os.Chmod(filepath.Join(dir, "a"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "a"), 0o755) })

	source := discovery.NewGlobalSource(dir)
	err := Run(context.Background(), source, "a", "b", map[string]string{}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected stat error")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("command a.yaml:b was executed despite stat error")
	}
}

// writeResolveFixture создаёт модуль с вложенной командой, листовой командой
// и default cmd (m) и модуль без default cmd (nodef).
func writeResolveFixture(t *testing.T) discovery.Source {
	t.Helper()
	dir := t.TempDir()
	m := `
name: m
cmd: "echo default"
params:
  - name: flag1
    type: bool
commands:
  leaf:
    params:
      - name: msg
        type: string
    cmd: "echo leaf"
  grp:
    commands:
      inner:
        cmd: "echo inner"
`
	nodef := `
name: nodef
commands:
  a:
    cmd: "echo a"
`
	for name, body := range map[string]string{"m.yaml": m, "nodef.yaml": nodef} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return discovery.NewGlobalSource(dir)
}

func TestResolveCommand(t *testing.T) {
	source := writeResolveFixture(t)

	tests := []struct {
		name      string
		namespace string
		command   string
		wantCmd   string
		wantParam string
	}{
		{"leaf", "m", "leaf", "echo leaf", "msg"},
		{"nested", "m.grp", "inner", "echo inner", ""},
		{"default cmd", "m", "", "echo default", "flag1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := ResolveCommand(source, nil, tt.namespace, tt.command)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Cmd != tt.wantCmd {
				t.Fatalf("Cmd = %q, want %q", cmd.Cmd, tt.wantCmd)
			}
			if tt.wantParam != "" && (len(cmd.Params) != 1 || cmd.Params[0].Name != tt.wantParam) {
				t.Fatalf("Params = %+v, want single %q", cmd.Params, tt.wantParam)
			}
		})
	}
}

func TestResolveCommandNoDefaultCmdSameErrorAsRun(t *testing.T) {
	source := writeResolveFixture(t)

	_, err := ResolveCommand(source, nil, "nodef", "")
	if err == nil || !strings.Contains(err.Error(), `module "nodef" has no default command`) {
		t.Fatalf("err = %v, want 'module \"nodef\" has no default command'", err)
	}
	runErr := Run(context.Background(), source, "nodef", "", nil, "", nil, nil, nil, nil)
	if runErr == nil || runErr.Error() != err.Error() {
		t.Fatalf("Run err = %v, want identical to %v", runErr, err)
	}
}

func TestResolveCommandModuleNotFound(t *testing.T) {
	source := writeResolveFixture(t)
	_, err := ResolveCommand(source, nil, "nonexistent", "cmd")
	if err == nil || !strings.Contains(err.Error(), "module not found") {
		t.Fatalf("err = %v, want module not found", err)
	}
}

// ResolveCommand даёт схему для исполнения: после правки манифеста на диске
// кэш с политикой force_only не должен отдавать устаревший список параметров.
func TestResolveCommandUsesVerifiedLoad(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "m.yaml")
	v1 := "name: m\ncommands:\n  c:\n    cmd: \"echo c\"\n"
	v2 := "name: m\ncommands:\n  c:\n    params:\n      - name: dry\n        type: bool\n    cmd: \"echo c\"\n"
	if err := os.WriteFile(manifest, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	source := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.ForceOnlyPolicy{})
	// Прогреваем кэш быстрой загрузкой (как list/help).
	if _, err := cache.LoadModule(manifest, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd, err := ResolveCommand(source, cache, "m", "c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmd.Params) != 1 || cmd.Params[0].Name != "dry" {
		t.Fatalf("Params = %+v, want fresh schema with param dry", cmd.Params)
	}
}

// Ошибка shlex не должна раскрывать отрендеренную команду (в ней могут быть секреты из env).
func TestRunSplitErrorDoesNotLeakRenderedCommand(t *testing.T) {
	t.Setenv("FUTI_T_SECRET", "s3cr3t")

	dir := t.TempDir()
	manifest := []byte(`
name: leak_mod
commands:
  deploy:
    params:
      - name: msg
        type: string
        required: true
    cmd: "echo SECRET-{{.env.FUTI_T_SECRET}} {{.msg}}"
`)
	if err := os.WriteFile(filepath.Join(dir, "leak_mod.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := discovery.NewGlobalSource(dir)
	err := Run(context.Background(), source, "leak_mod", "deploy", map[string]string{"msg": "it's"}, "", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected split error, got nil")
	}
	msg := err.Error()
	for _, leaked := range []string{"s3cr3t", "SECRET-", "echo"} {
		if strings.Contains(msg, leaked) {
			t.Errorf("error leaks %q: %v", leaked, err)
		}
	}
	for _, want := range []string{"leak_mod", "deploy", "EOF found when expecting closing quote"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
}

// TestRunModuleNameDefaultsToPath: без name: {{.module_name}} и
// FUTI_MODULE_NAME равны имени файла/каталога модуля, с name: — ему.
func TestRunModuleNameDefaultsToPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported in this test on Windows")
	}
	tests := []struct {
		name  string
		path  string // относительно каталога модулей
		extra string
		want  string
	}{
		{"файл без name", "deploy.yaml", "", "deploy deploy\n"},
		{"пакет без name", "deploy/config.yaml", "", "deploy deploy\n"},
		{"файл с name", "deploy.yaml", "name: x\n", "x x\n"},
		{"пакет с name", "deploy/config.yaml", "name: x\n", "x x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			full := filepath.Join(dir, filepath.FromSlash(tt.path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			body := tt.extra + "cmd: \"sh -c 'echo {{.module_name}} $FUTI_MODULE_NAME'\"\n"
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err := Run(context.Background(), discovery.NewGlobalSource(dir), "deploy", "", nil, "", nil, &out, nil, nil)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if out.String() != tt.want {
				t.Fatalf("output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

// writeModules пишет манифесты (путь относительно dir -> тело) и возвращает глобальный source.
func writeModules(t *testing.T, files map[string]string) discovery.Source {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return discovery.NewGlobalSource(dir)
}

func TestResolveCommandDotNamespaceWithoutCommand(t *testing.T) {
	const docker = "name: docker\ncommands:\n  build:\n    cmd: \"echo docker-build\"\n"
	const devDeploy = "name: dev.deploy\ncmd: \"echo deploy-default\"\n"
	const dockerBuildMod = "name: docker.build\ncmd: \"echo module-docker-build\"\n"

	tests := []struct {
		name      string
		files     map[string]string
		namespace string
		wantCmd   string
	}{
		{"dot split to module and command", map[string]string{"docker.yaml": docker}, "docker.build", "echo docker-build"},
		{"module with dotted name keeps default cmd", map[string]string{"dev/deploy.yaml": devDeploy}, "dev.deploy", "echo deploy-default"},
		{"longest module wins", map[string]string{"docker.yaml": docker, "docker/build.yaml": dockerBuildMod}, "docker.build", "echo module-docker-build"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := writeModules(t, tt.files)
			cmd, err := ResolveCommand(source, nil, tt.namespace, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Cmd != tt.wantCmd {
				t.Fatalf("Cmd = %q, want %q", cmd.Cmd, tt.wantCmd)
			}
		})
	}
}

func TestRunDotNamespaceWithoutCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("echo недоступен на Windows")
	}
	source := writeModules(t, map[string]string{
		"docker.yaml": "name: docker\ncommands:\n  build:\n    cmd: \"echo built\"\n",
	})
	var out bytes.Buffer
	if err := Run(context.Background(), source, "docker.build", "", nil, "", nil, &out, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "built\n" {
		t.Fatalf("output = %q, want %q", got, "built\n")
	}
}

func TestListCommandsSortedByName(t *testing.T) {
	source := writeModules(t, map[string]string{
		"m.yaml": "name: m\ncommands:\n  zeta:\n    cmd: \"echo z\"\n  alpha:\n    cmd: \"echo a\"\n  mid:\n    cmd: \"echo m\"\n  beta:\n    cmd: \"echo b\"\n",
	})
	// Порядок обхода map случаен — несколько прогонов повышают шанс поймать регресс.
	for i := 0; i < 20; i++ {
		cmds, err := ListCommands(source, "m", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var got []string
		for _, c := range cmds {
			got = append(got, c.Name)
		}
		if want := "alpha,beta,mid,zeta"; strings.Join(got, ",") != want {
			t.Fatalf("names = %v, want %s", got, want)
		}
	}
}

// writePrepFixture пишет модуль prep с командой mk (required-параметр file) в dir.
func writePrepFixture(t *testing.T, dir string) discovery.Source {
	t.Helper()
	manifest := []byte(`
name: prep
commands:
  mk:
    params:
      - name: file
        type: string
        required: true
    cmd: "touch {{quote .file}}"
`)
	if err := os.WriteFile(filepath.Join(dir, "prep.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return discovery.NewGlobalSource(dir)
}

// TestPrepareReturnsArgvWithoutExec: Prepare отдаёт argv и метаданные модуля,
// ничего не исполняя.
func TestPrepareReturnsArgvWithoutExec(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	target := filepath.Join(work, "a b'c")
	source := writePrepFixture(t, dir)

	p, err := Prepare(source, "prep", "mk", map[string]string{"file": target}, "", nil, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	want := []string{"touch", target}
	if !slices.Equal(p.Argv, want) {
		t.Fatalf("Argv = %q, want %q", p.Argv, want)
	}
	if p.ModuleName != "prep" {
		t.Errorf("ModuleName = %q, want %q", p.ModuleName, "prep")
	}
	gotRoot, _ := filepath.EvalSymlinks(p.ModuleRoot)
	wantRoot, _ := filepath.EvalSymlinks(dir)
	if gotRoot != wantRoot {
		t.Errorf("ModuleRoot = %q, want %q", p.ModuleRoot, dir)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("файл создан, Prepare не должен исполнять команду (stat err = %v)", err)
	}
}

func TestPrepareValidationError(t *testing.T) {
	dir := t.TempDir()
	source := writePrepFixture(t, dir)

	p, err := Prepare(source, "prep", "mk", map[string]string{}, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), `required param "file" missing`) {
		t.Errorf("err = %v, want mention of missing required param \"file\"", err)
	}
	if p != nil {
		t.Errorf("Prepared = %+v, want nil", p)
	}
}
