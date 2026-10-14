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

func TestValidateSchemaRules(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string // подстрока ожидаемой ошибки; пусто — ошибок быть не должно
	}{
		{"module params without cmd", "params:\n  - name: a\ncommands:\n  x:\n    cmd: echo\n", `command "(default)": params/positional require cmd`},
		{"module positional without cmd", "positional:\n  min: 1\ncommands:\n  x:\n    cmd: echo\n", `command "(default)": params/positional require cmd`},
		{"branch params without cmd", "commands:\n  b:\n    params:\n      - name: a\n    commands:\n      c:\n        cmd: echo\n", `command "b": params/positional require cmd`},
		{"blank cmd", "commands:\n  b:\n    cmd: \"  \"\n", `command "b": must have cmd or commands`},
		{"user command named (default)", "commands:\n  \"(default)\": {}\n", `command "(default)": must have cmd or commands`},
		{"blank default cmd with params", "cmd: \"  \"\nparams:\n  - name: a\ncommands:\n  x:\n    cmd: echo\n", `command "(default)": params/positional require cmd`},
		{"broken cmd template", "commands:\n  b:\n    cmd: 'echo {{.msg'\n", `command "b": invalid cmd template:`},
		{"broken default template", "cmd: 'echo {{.msg'\n", `command "(default)": invalid cmd template:`},
		{"quote func ok", "commands:\n  b:\n    cmd: '{{quote .x}}'\n    params:\n      - name: x\n", ""},
		{"broken attach", "attach: '{{.x'\ncmd: echo\n", `attach: invalid template:`},
		{"broken detach", "detach: '{{.x'\ncmd: echo\n", `detach: invalid template:`},
		{"name starts with dash", "commands:\n  -x:\n    cmd: echo\n", `command "-x": invalid command name`},
		{"name with dot", "commands:\n  a.b:\n    cmd: echo\n", `command "a.b": invalid command name`},
		{"name with space", "commands:\n  \"a b\":\n    cmd: echo\n", `command "a b": invalid command name`},
		{"empty name", "commands:\n  \"\":\n    cmd: echo\n", `command "": invalid command name`},
		{"required with default", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: x\n        required: true\n        default: v\n", `command "b": param "x": required and default are mutually exclusive`},
		{"required without default", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: x\n        required: true\n", ""},
		{"int default not int", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: x\n        type: int\n        default: abc\n", `command "b": param "x": default "abc" is not an int`},
		{"int default ok", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: x\n        type: int\n        default: \"10\"\n", ""},
		{"bool default maybe", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: x\n        type: bool\n        default: maybe\n", `command "b": param "x": default "maybe" is not a boolean (true/false, 1/0, yes/no, on/off)`},
		{"bool default false", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: x\n        type: bool\n        default: \"false\"\n", ""},
		{"bool default off", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: x\n        type: bool\n        default: \"off\"\n", ""},
		{"default cmd int default bad", "cmd: echo\nparams:\n  - name: x\n    type: int\n    default: abc\n", `command "(default)": param "x": default "abc" is not an int`},
		{"nested bad name", "commands:\n  p:\n    cmd: echo\n    commands:\n      \"q r\":\n        cmd: echo\n", `command "p.q r": invalid command name`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAndValidate(t, tc.manifest)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("errors = %q, want none", got)
				}
				return
			}
			for _, e := range got {
				if strings.Contains(e, tc.want) {
					return
				}
			}
			t.Fatalf("errors = %q, want one containing %q", got, tc.want)
		})
	}
}

// TestValidateReservedListCommand: имя list на любом уровне — ошибка схемы
// (команда недостижима: list последним словом пути — системный запрос);
// help и validate зарезервированными не считаются.
func TestValidateReservedListCommand(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     []string
	}{
		{"top level", "commands:\n  list:\n    cmd: echo\n", []string{`command "list": "list" is reserved (use a different command name)`}},
		{"nested", "commands:\n  a:\n    commands:\n      list:\n        cmd: echo\n", []string{`command "a.list": "list" is reserved (use a different command name)`}},
		{"parent before nested", "commands:\n  list:\n    commands:\n      list:\n        cmd: echo\n", []string{
			`command "list": "list" is reserved (use a different command name)`,
			`command "list.list": "list" is reserved (use a different command name)`,
		}},
		{"help allowed", "commands:\n  help:\n    cmd: echo\n", nil},
		{"validate allowed", "commands:\n  validate:\n    cmd: echo\n", nil},
		{"list as param name allowed", "commands:\n  x:\n    cmd: echo\n    params:\n      - name: list\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAndValidate(t, tc.manifest)
			if len(got) != len(tc.want) {
				t.Fatalf("errors = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("errors[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestLoadModuleRejectsListCommand: модуль с командой list не загружается.
func TestLoadModuleRejectsListCommand(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bad.yaml")
	writeFile(t, manifestPath, "commands:\n  list:\n    cmd: echo\n")
	if _, err := LoadModule(manifestPath, dir); err == nil || !strings.Contains(err.Error(), `"list" is reserved`) {
		t.Fatalf("err = %v, want '\"list\" is reserved'", err)
	}
}

func TestValidateOrderIsDeterministic(t *testing.T) {
	mod := &Module{Commands: map[string]*Command{
		"d": {}, "c": {}, "b": {}, "a": {},
	}}
	first, _ := mod.Validate()
	if len(first) != 4 || first[0].Error() != `command "a": must have cmd or commands` {
		t.Fatalf("first = %v, want 4 errors starting with command a", first)
	}
	for i := 0; i < 20; i++ {
		got, _ := mod.Validate()
		for j := range first {
			if got[j].Error() != first[j].Error() {
				t.Fatalf("run %d: errors differ at %d: %v vs %v", i, j, got, first)
			}
		}
	}
}

func TestValidateOrderSectionsAndParentBeforeChildren(t *testing.T) {
	mod := &Module{
		Attach:   "{{.x",
		Cmd:      "echo",
		Params:   []Param{{Name: "p", Type: "float"}},
		Commands: map[string]*Command{"b": {Cmd: "echo", Commands: map[string]*Command{"z": {}, "y": {}}}, "a": {}},
	}
	errs, _ := mod.Validate()
	var got []string
	for _, e := range errs {
		got = append(got, e.Error())
	}
	prefixes := []string{`attach:`, `command "(default)"`, `command "a"`, `command "b.y"`, `command "b.z"`}
	if len(got) != len(prefixes) {
		t.Fatalf("errors = %q, want %d", got, len(prefixes))
	}
	for i, p := range prefixes {
		if !strings.HasPrefix(got[i], p) {
			t.Fatalf("errors[%d] = %q, want prefix %q (all: %q)", i, got[i], p, got)
		}
	}
}

func TestCommandValidateUnknownParamIsLexicographicallyFirst(t *testing.T) {
	c := &Command{}
	for i := 0; i < 20; i++ {
		_, err := c.Validate(map[string]string{"z": "1", "a": "2", "m": "3"})
		if err == nil || !strings.Contains(err.Error(), `"a"`) {
			t.Fatalf("err = %v, want error about \"a\"", err)
		}
	}
}

func TestValidateEmptyCmdWithParamsNoDuplicateErrors(t *testing.T) {
	got := parseAndValidate(t, "params:\n  - name: a\n")
	want := []string{
		"module has no cmd and no commands",
		`command "(default)": params/positional require cmd`,
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}
