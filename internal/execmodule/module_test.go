package execmodule

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLoadModuleParsesCommands(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "docker.yaml")
	writeFile(t, manifestPath, `
name: docker
description: Docker management
commands:
  ps:
    description: List containers
    cmd: "docker ps"
  build:
    description: Build image
    params:
      - name: tag
        type: string
        required: true
      - name: file
        type: string
        default: Dockerfile
    cmd: "docker build -t {{.tag}} -f {{.file}} ."
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Name != "docker" {
		t.Fatalf("unexpected name: %q", mod.Name)
	}
	if mod.Description != "Docker management" {
		t.Fatalf("unexpected description: %q", mod.Description)
	}
	if mod.Root != dir {
		t.Fatalf("unexpected root: %q", mod.Root)
	}
	if len(mod.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(mod.Commands))
	}

	ps, ok := mod.Commands["ps"]
	if !ok {
		t.Fatal("command 'ps' not found")
	}
	if ps.Cmd != "docker ps" {
		t.Fatalf("unexpected ps cmd: %q", ps.Cmd)
	}
	if ps.Description != "List containers" {
		t.Fatalf("unexpected ps description: %q", ps.Description)
	}

	build, ok := mod.Commands["build"]
	if !ok {
		t.Fatal("command 'build' not found")
	}
	if len(build.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(build.Params))
	}
	if build.Params[0].Name != "tag" || !build.Params[0].Required {
		t.Fatalf("unexpected param 0: %+v", build.Params[0])
	}
	if build.Params[1].Name != "file" || build.Params[1].Default != "Dockerfile" {
		t.Fatalf("unexpected param 1: %+v", build.Params[1])
	}
}

func TestLoadModuleMissingCommand(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "empty.yaml")
	writeFile(t, manifestPath, "name: one\ncommands:\n  x:\n    cmd: echo\n")

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := mod.Commands["nonexistent"]; ok {
		t.Fatal("expected command not found, but it exists")
	}
}

func TestLoadModuleInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bad.yaml")
	writeFile(t, manifestPath, `name: bad\n  :: invalid`)

	_, err := LoadModule(manifestPath, dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadModuleWithDefaultCmd(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	writeFile(t, manifestPath, `
name: echo_mod
description: Echo utilities
cmd: "echo {{.args}}"
commands:
  upper:
    cmd: "echo {{.args}} | tr a-z A-Z"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Cmd != "echo {{.args}}" {
		t.Fatalf("unexpected default cmd: %q", mod.Cmd)
	}
	if mod.Description != "Echo utilities" {
		t.Fatalf("unexpected description: %q", mod.Description)
	}
	if len(mod.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(mod.Commands))
	}
}

func TestLoadModuleWithoutDefaultCmd(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeFile(t, manifestPath, `
name: db
commands:
  status:
    cmd: "echo status"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Cmd != "" {
		t.Fatalf("expected empty default cmd, got %q", mod.Cmd)
	}
}

func TestLoadModuleWithSubcommands(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeFile(t, manifestPath, `
name: db
commands:
  migrate:
    description: Database migrations
    commands:
      up:
        description: Run migrations up
        cmd: "echo migrating up"
      down:
        description: Run migrations down
        cmd: "echo migrating down"
      create:
        description: Create a new migration
        params:
          - name: name
            type: string
            required: true
        cmd: "echo create {{.name}}"
  status:
    cmd: "echo status"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	migrate, ok := mod.Commands["migrate"]
	if !ok {
		t.Fatal("command 'migrate' not found")
	}
	if migrate.Description != "Database migrations" {
		t.Fatalf("unexpected description: %q", migrate.Description)
	}
	if len(migrate.Commands) != 3 {
		t.Fatalf("expected 3 subcommands, got %d", len(migrate.Commands))
	}

	up, ok := migrate.Commands["up"]
	if !ok {
		t.Fatal("subcommand 'up' not found")
	}
	if up.Cmd != "echo migrating up" {
		t.Fatalf("unexpected up cmd: %q", up.Cmd)
	}
	if up.Description != "Run migrations up" {
		t.Fatalf("unexpected up description: %q", up.Description)
	}

	create, ok := migrate.Commands["create"]
	if !ok {
		t.Fatal("subcommand 'create' not found")
	}
	if len(create.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(create.Params))
	}
	if create.Params[0].Name != "name" || !create.Params[0].Required {
		t.Fatalf("unexpected param: %+v", create.Params[0])
	}

	status, ok := mod.Commands["status"]
	if !ok {
		t.Fatal("command 'status' not found")
	}
	if status.Cmd != "echo status" {
		t.Fatalf("unexpected status cmd: %q", status.Cmd)
	}
	if len(status.Commands) != 0 {
		t.Fatalf("expected 0 subcommands, got %d", len(status.Commands))
	}
}

func TestLoadModuleNestedSubcommands(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeFile(t, manifestPath, `
name: db
commands:
  migrate:
    commands:
      up:
        commands:
          force:
            cmd: "echo force"
          safe:
            cmd: "echo safe"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	migrate := mod.Commands["migrate"]
	up := migrate.Commands["up"]
	if len(up.Commands) != 2 {
		t.Fatalf("expected 2 nested subcommands, got %d", len(up.Commands))
	}
	if up.Commands["force"].Cmd != "echo force" {
		t.Fatal("nested subcommand 'force' not parsed correctly")
	}
	if up.Commands["safe"].Cmd != "echo safe" {
		t.Fatal("nested subcommand 'safe' not parsed correctly")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}

func TestLoadModuleParsesPositional(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "greet.yaml")
	writeFile(t, manifestPath, `
name: greet
commands:
  hello:
    cmd: "echo {{index .args_list 0}}"
    positional:
      min: 1
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hello := mod.Commands["hello"]
	if hello == nil {
		t.Fatal("command 'hello' not found")
	}
	if hello.Positional.Min != 1 {
		t.Fatalf("unexpected positional min: %d, want 1", hello.Positional.Min)
	}
}

func TestLoadModulePositionalDefaultZero(t *testing.T) {
	// Если positional не задан — min должен быть 0 (по умолчанию).
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "mod.yaml")
	writeFile(t, manifestPath, `
name: mod
commands:
  run:
    cmd: "echo hi"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mod.Commands["run"].Positional.Min; got != 0 {
		t.Fatalf("unexpected positional min: %d, want 0", got)
	}
}

func TestLoadModuleParsesParamDescription(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "mod.yaml")
	writeFile(t, manifestPath, `
name: mod
commands:
  run:
    params:
      - name: tag
        type: string
        required: true
        description: Image tag
    cmd: "echo {{.tag}}"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	run := mod.Commands["run"]
	if run.Params[0].Description != "Image tag" {
		t.Fatalf("unexpected param description: %q", run.Params[0].Description)
	}
}

func TestLoadModuleDefaultCmdParams(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "meta.yaml")
	writeFile(t, manifestPath, `
name: meta
cmd: "echo {{.sep}} {{.args}}"
params:
  - name: sep
    type: string
    default: "|"
  - name: upper
    type: bool
    default: "false"
positional:
  min: 1
commands:
  show:
    cmd: "echo show"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mod.Params) != 2 {
		t.Fatalf("expected 2 default cmd params, got %d", len(mod.Params))
	}
	if mod.Params[0].Name != "sep" || mod.Params[0].Default != "|" {
		t.Fatalf("unexpected first param: %+v", mod.Params[0])
	}
	if mod.Params[1].Name != "upper" || mod.Params[1].Type != "bool" {
		t.Fatalf("unexpected second param: %+v", mod.Params[1])
	}
	if mod.Positional.Min != 1 {
		t.Fatalf("unexpected default positional min: %d", mod.Positional.Min)
	}
}

func TestParseModuleAttachDetach(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "qr.yaml")
	writeFile(t, manifestPath, `
name: qr
description: QR codes
attach: ".venv/bin/python -m pip install -r requirements.txt"
detach: "rm -rf .venv"
commands:
  make:
    cmd: "echo make"
`)

	mod, err := ParseModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Attach != ".venv/bin/python -m pip install -r requirements.txt" {
		t.Fatalf("unexpected attach: %q", mod.Attach)
	}
	if mod.Detach != "rm -rf .venv" {
		t.Fatalf("unexpected detach: %q", mod.Detach)
	}
}

func TestParseModuleAttachDetachAbsent(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "plain.yaml")
	writeFile(t, manifestPath, `
name: plain
commands:
  run:
    cmd: "echo run"
`)

	mod, err := ParseModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Attach != "" {
		t.Fatalf("expected empty attach, got %q", mod.Attach)
	}
	if mod.Detach != "" {
		t.Fatalf("expected empty detach, got %q", mod.Detach)
	}
}

func TestParseModuleRejectsFIFOAndHugeFile(t *testing.T) {
	t.Run("fifo", func(t *testing.T) {
		fifo := filepath.Join(t.TempDir(), "x.yaml")
		if err := syscall.Mkfifo(fifo, 0o644); err != nil {
			t.Fatalf("mkfifo: %v", err)
		}
		t.Cleanup(func() {
			if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
				f.Close()
			}
		})
		errCh := make(chan error, 1)
		go func() {
			_, err := ParseModule(fifo, filepath.Dir(fifo))
			errCh <- err
		}()
		select {
		case err := <-errCh:
			if err == nil {
				t.Fatal("ParseModule(FIFO): ожидалась ошибка")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("ParseModule завис на FIFO")
		}
	})

	const limit = 1 << 20
	t.Run("больше лимита", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "big.yaml")
		writeFile(t, path, "# "+strings.Repeat("a", limit-2)+"\nname: big\n")
		if info, _ := os.Stat(path); info.Size() <= limit {
			t.Fatalf("размер файла %d, нужно > %d", info.Size(), limit)
		}
		_, err := ParseModule(path, filepath.Dir(path))
		if err == nil || !strings.Contains(err.Error(), "размер") {
			t.Fatalf("ParseModule(файл > 1 МиБ): err = %v, want ошибка с \"размер\"", err)
		}
	})

	t.Run("ровно лимит", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ok.yaml")
		head := "name: ok\n# "
		writeFile(t, path, head+strings.Repeat("a", limit-len(head)))
		if info, _ := os.Stat(path); info.Size() != limit {
			t.Fatalf("размер файла %d, want %d", info.Size(), limit)
		}
		mod, err := ParseModule(path, filepath.Dir(path))
		if err != nil {
			t.Fatalf("ParseModule(файл ровно в лимит): %v", err)
		}
		if mod.Name != "ok" {
			t.Fatalf("Name = %q, want %q", mod.Name, "ok")
		}
	})
}

// parseAndValidate разбирает манифест из строки и возвращает тексты ошибок Validate.
func parseAndValidate(t *testing.T, manifest string) []string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	writeFile(t, p, manifest)
	mod, err := ParseModule(p, dir)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}
	errs, _ := mod.Validate()
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

func TestParseModuleUnknownKeysReportedFirstByValidate(t *testing.T) {
	got := parseAndValidate(t, `
comands:
  x: {}
commands:
  build:
    cmd: echo
    params:
      - name: tag
        requird: true
`)
	want := []string{
		`line 2: unknown key "comands"`,
		`line 9: unknown key "requird" in commands.build.params[0]`,
	}
	if len(got) < 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("errors = %q, want prefix %q", got, want)
	}
}

func TestLoadModuleFailsOnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	writeFile(t, p, "cmd: echo\nrequird: 1\n")
	_, err := LoadModule(p, dir)
	if err == nil || !strings.Contains(err.Error(), `unknown key "requird"`) {
		t.Fatalf("LoadModule err = %v, want unknown key error", err)
	}
}

func TestParseModuleAllowsXKeysAndMerge(t *testing.T) {
	got := parseAndValidate(t, `
x-common: &c
  description: shared
  cmd: echo
commands:
  a:
    <<: *c
  x-b:
    cmd: echo
`)
	if len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
}

func TestParseModuleEmptyManifestIsEmptyModule(t *testing.T) {
	got := parseAndValidate(t, "")
	if len(got) != 1 || got[0] != "module has no cmd and no commands" {
		t.Fatalf("errors = %q, want [module has no cmd and no commands]", got)
	}
}
