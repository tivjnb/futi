package execmodule

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateValidModule проверяет, что валидный модуль не даёт ошибок
// и предупреждений.
func TestValidateValidModule(t *testing.T) {
	mod := &Module{
		Name: "docker",
		Cmd:  "docker ps",
		Commands: map[string]*Command{
			"build": {
				Cmd: "docker build",
				Params: []Param{
					{Name: "tag", Type: "string", Required: true},
					{Name: "file", Type: "string", Default: "Dockerfile"},
				},
			},
			"logs": {
				Cmd: "docker logs",
				Params: []Param{
					{Name: "container", Type: "string", Required: true},
					{Name: "follow", Type: "bool", Default: "false"},
				},
			},
		},
	}

	errors, warnings := mod.Validate()
	if len(errors) != 0 {
		t.Fatalf("expected 0 errors, got %d: %v", len(errors), errors)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected 0 warnings, got %d: %v", len(warnings), warnings)
	}
}

// TestValidateCommandWithoutCmdAndCommands проверяет, что команда без cmd
// и без commands — ошибка (бесполезная команда).
func TestValidateCommandWithoutCmdAndCommands(t *testing.T) {
	mod := &Module{
		Name: "bad",
		Commands: map[string]*Command{
			"empty": {}, // ни cmd, ни commands
		},
	}

	errors, _ := mod.Validate()
	if len(errors) == 0 {
		t.Fatal("expected error for command without cmd and commands, got none")
	}
}

// TestValidateDuplicateParams проверяет, что дубликаты имён params в команде — ошибка.
func TestValidateDuplicateParams(t *testing.T) {
	mod := &Module{
		Name: "bad",
		Commands: map[string]*Command{
			"build": {
				Cmd: "docker build",
				Params: []Param{
					{Name: "tag", Type: "string"},
					{Name: "tag", Type: "string"}, // дубликат
				},
			},
		},
	}

	errors, _ := mod.Validate()
	if len(errors) == 0 {
		t.Fatal("expected error for duplicate param names, got none")
	}
}

// TestValidateInvalidType проверяет, что type ∉ {string, bool, int, ""} — ошибка.
func TestValidateInvalidType(t *testing.T) {
	mod := &Module{
		Name: "bad",
		Commands: map[string]*Command{
			"build": {
				Cmd: "docker build",
				Params: []Param{
					{Name: "tag", Type: "float"}, // невалидный тип
				},
			},
		},
	}

	errors, _ := mod.Validate()
	if len(errors) == 0 {
		t.Fatal("expected error for invalid param type, got none")
	}
}

// TestValidateEmptyTypeIsValid проверяет, что пустой type валиден (= string).
func TestValidateEmptyTypeIsValid(t *testing.T) {
	mod := &Module{
		Name: "ok",
		Commands: map[string]*Command{
			"build": {
				Cmd: "docker build",
				Params: []Param{
					{Name: "tag"}, // Type="" — валидно (string по умолчанию)
				},
			},
		},
	}

	errors, _ := mod.Validate()
	if len(errors) != 0 {
		t.Fatalf("expected 0 errors for empty type, got %d: %v", len(errors), errors)
	}
}

// TestValidateCommandWithCommandsButNoCmd проверяет, что команда с commands,
// но без cmd — предупреждение (если нет default cmd на модуле).
func TestValidateCommandWithCommandsButNoCmd(t *testing.T) {
	mod := &Module{
		Name: "db", // нет Cmd на уровне модуля
		Commands: map[string]*Command{
			"migrate": {
				// нет Cmd, но есть commands — ветвь без fallback
				Commands: map[string]*Command{
					"up":   {Cmd: "migrate up"},
					"down": {Cmd: "migrate down"},
				},
			},
		},
	}

	errors, warnings := mod.Validate()
	if len(errors) != 0 {
		t.Fatalf("expected 0 errors (command with commands is valid), got %d: %v", len(errors), errors)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for command with commands but no cmd, got none")
	}
}

// TestValidateCommandWithCommandsAndNoCmdNoWarningIfModuleHasDefault проверяет,
// что предупреждения нет, если у модуля есть default cmd.
// Примечание: warning про команду без cmd, не про модуль. Если у команды есть
// commands и cmd — нет warning. Если у команды есть только commands — warning
// независимо от модуля, т.к. вызов этой команды без subcommand всё равно падает.
// Этот тест фиксирует: команда с commands и cmd — нет warning.
func TestValidateCommandWithCommandsAndCmdNoWarning(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"migrate": {
				Cmd: "migrate", // fallback
				Commands: map[string]*Command{
					"up":   {Cmd: "migrate up"},
					"down": {Cmd: "migrate down"},
				},
			},
		},
	}

	errors, warnings := mod.Validate()
	if len(errors) != 0 {
		t.Fatalf("expected 0 errors, got %d: %v", len(errors), errors)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected 0 warnings (command has cmd fallback), got %d: %v", len(warnings), warnings)
	}
}

// TestValidateNestedSubcommands проверяет рекурсивную валидацию subcommands.
func TestValidateNestedSubcommands(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {
						Commands: map[string]*Command{
							"force": {}, // невалидна: нет cmd и commands
						},
					},
				},
			},
		},
	}

	errors, _ := mod.Validate()
	if len(errors) == 0 {
		t.Fatal("expected error for nested invalid subcommand, got none")
	}
}

// TestValidateCollectsAllErrors проверяет, что Validate возвращает ВСЕ ошибки,
// а не падает на первой.
func TestValidateCollectsAllErrors(t *testing.T) {
	mod := &Module{
		Name: "bad",
		Commands: map[string]*Command{
			"empty": {}, // ошибка 1: без cmd и commands
			"build": {
				Cmd: "docker build",
				Params: []Param{
					{Name: "tag", Type: "float"},  // ошибка 2: невалидный тип
					{Name: "tag", Type: "string"}, // ошибка 3: дубликат
				},
			},
		},
	}

	errors, _ := mod.Validate()
	if len(errors) < 3 {
		t.Fatalf("expected at least 3 errors, got %d: %v", len(errors), errors)
	}
}

// TestLoadModuleReturnsErrorForInvalidModule проверяет, что LoadModule
// автоматически валидирует модуль и возвращает ошибку для невалидного.
func TestLoadModuleReturnsErrorForInvalidModule(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bad.yaml")
	writeFile(t, manifestPath, `
name: bad
commands:
  empty: {}
`)

	_, err := LoadModule(manifestPath, dir)
	if err == nil {
		t.Fatal("expected error for invalid module (command without cmd and commands), got nil")
	}
}

// TestLoadModuleValidModuleNoError проверяет, что валидный модуль
// загружается без ошибок.
func TestLoadModuleValidModuleNoError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "ok.yaml")
	writeFile(t, manifestPath, `
name: ok
cmd: "echo {{.args}}"
commands:
  greet:
    params:
      - name: msg
        type: string
        required: true
    cmd: "echo {{.msg}}"
`)

	_, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error for valid module: %v", err)
	}
}

func TestValidateNegativePositionalMin(t *testing.T) {
	mod := &Module{
		Name: "bad",
		Commands: map[string]*Command{
			"run": {
				Cmd:        "echo hi",
				Positional: Positional{Min: -1},
			},
		},
	}
	errors, warnings := mod.Validate()
	if len(errors) == 0 {
		t.Fatal("expected error for negative positional min")
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

func TestValidateDefaultCmdParams(t *testing.T) {
	mod := &Module{
		Name: "meta",
		Cmd:  "echo {{.json}}",
		Params: []Param{
			{Name: "json", Type: "bool"},
			{Name: "json", Type: "bool"},
			{Name: "scale", Type: "float"},
		},
		Positional: Positional{Min: -1},
	}

	errors, _ := mod.Validate()
	if len(errors) != 3 {
		t.Fatalf("expected 3 errors (duplicate param, invalid type, negative min), got %d: %v", len(errors), errors)
	}
}

// TestValidateReservedParamNames: зарезервированные имена params — ошибка
// схемы в команде, вложенной команде и default-команде модуля.
func TestValidateReservedParamNames(t *testing.T) {
	for _, name := range []string{"args", "args_list", "env", "module_name", "module_root"} {
		p := []Param{{Name: name}}
		cases := map[string]*Module{
			"command": {Commands: map[string]*Command{"run": {Cmd: "x", Params: p}}},
			"nested": {Commands: map[string]*Command{"grp": {Commands: map[string]*Command{
				"run": {Cmd: "x", Params: p},
			}}}},
			"default": {Cmd: "x", Params: p},
		}
		for kind, mod := range cases {
			errs, _ := mod.Validate()
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), "is reserved") {
				t.Errorf("%s/%s: errors = %v, want one 'is reserved'", kind, name, errs)
			}
		}
	}
}

// TestValidateParamNameFormat: имя param должно соответствовать
// ^[A-Za-z_][A-Za-z0-9_-]*$.
func TestValidateParamNameFormat(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"", true}, {"1x", true}, {"-x", true}, {"a b", true}, {"a.b", true}, {"a=b", true},
		{"dry-run", false}, {"_x", false}, {"X1", false},
	}
	for _, c := range cases {
		mod := &Module{Commands: map[string]*Command{"run": {Cmd: "x", Params: []Param{{Name: c.name}}}}}
		errs, _ := mod.Validate()
		if c.wantErr {
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), "invalid param name") {
				t.Errorf("%q: errors = %v, want one 'invalid param name'", c.name, errs)
			}
		} else if len(errs) != 0 {
			t.Errorf("%q: unexpected errors %v", c.name, errs)
		}
	}
}

// TestLoadModuleRejectsReservedParam: модуль с param env не загружается.
func TestLoadModuleRejectsReservedParam(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bad.yaml")
	writeFile(t, manifestPath, `
name: bad
commands:
  deploy:
    params:
      - name: env
    cmd: "deploy {{.env}}"
`)
	_, err := LoadModule(manifestPath, dir)
	if err == nil || !strings.Contains(err.Error(), "is reserved") {
		t.Fatalf("err = %v, want 'is reserved'", err)
	}
}
