package execmodule

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/inputs"
)

// cmdText is a string-form cmd.
func cmdText(s string) CmdSpec { return CmdSpec{Text: s} }

// strPtr returns a pointer to s (for Spec.Default).
func strPtr(s string) *string { return &s }

// TestValidateValidModule проверяет, что валидный модуль не даёт ошибок
// и предупреждений.
func TestValidateValidModule(t *testing.T) {
	mod := &Module{
		Name: "docker",
		Command: Command{
			Cmd: cmdText("docker ps"),
			Commands: map[string]*Command{
				"build": {
					Cmd: cmdText("docker build {{.tag}} {{.file}}"),
					Flags: []inputs.Spec{
						{Name: "tag", Required: true, Flag: true},
						{Name: "file", Default: strPtr("Dockerfile"), Flag: true},
					},
				},
				"logs": {
					Cmd:  cmdText("docker logs {{if .follow}}-f{{end}} {{.container}}"),
					Args: []inputs.Spec{{Name: "container", Required: true}},
					Flags: []inputs.Spec{
						{Name: "follow", Type: "bool", Flag: true},
					},
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
		Command: Command{Commands: map[string]*Command{
			"empty": {}, // ни cmd, ни commands
		}},
	}

	errors, _ := mod.Validate()
	if len(errors) == 0 {
		t.Fatal("expected error for command without cmd and commands, got none")
	}
}

// TestValidateCommandWithCommandsButNoCmd проверяет, что команда с commands,
// но без cmd — предупреждение.
func TestValidateCommandWithCommandsButNoCmd(t *testing.T) {
	mod := &Module{
		Name: "db",
		Command: Command{Commands: map[string]*Command{
			"migrate": {
				// нет Cmd, но есть commands — ветвь без fallback
				Commands: map[string]*Command{
					"up":   {Cmd: cmdText("migrate up")},
					"down": {Cmd: cmdText("migrate down")},
				},
			},
		}},
	}

	errors, warnings := mod.Validate()
	if len(errors) != 0 {
		t.Fatalf("expected 0 errors (command with commands is valid), got %d: %v", len(errors), errors)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `command "migrate": has subcommands but no cmd`) {
		t.Fatalf("warnings = %q, want one about command migrate without cmd", warnings)
	}
}

// TestValidateCommandWithCommandsAndCmdNoWarning: команда с commands и cmd —
// без предупреждения.
func TestValidateCommandWithCommandsAndCmdNoWarning(t *testing.T) {
	mod := &Module{
		Name: "db",
		Command: Command{Commands: map[string]*Command{
			"migrate": {
				Cmd: cmdText("migrate"), // fallback
				Commands: map[string]*Command{
					"up":   {Cmd: cmdText("migrate up")},
					"down": {Cmd: cmdText("migrate down")},
				},
			},
		}},
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
		Command: Command{Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {
						Commands: map[string]*Command{
							"force": {}, // невалидна: нет cmd и commands
						},
					},
				},
			},
		}},
	}

	errors, _ := mod.Validate()
	if len(errors) != 1 || errors[0].Error() != `command "migrate.up.force": must have cmd or commands` {
		t.Fatalf("errors = %v, want one for migrate.up.force", errors)
	}
}

// TestValidateCollectsAllErrors проверяет, что Validate возвращает ВСЕ ошибки,
// а не падает на первой.
func TestValidateCollectsAllErrors(t *testing.T) {
	mod := &Module{
		Name: "bad",
		Command: Command{Commands: map[string]*Command{
			"empty": {}, // ошибка 1: без cmd и commands
			"build": {
				Cmd: cmdText("docker build {{.tag}}"),
				Args: []inputs.Spec{
					{Name: "tag", Type: "float"}, // ошибка 2: невалидный тип
					{Name: "tag"},                // ошибка 3: дубликат
				},
			},
		}},
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
cmd: "echo {{.words}}"
args:
  - name: words
    variadic: true
commands:
  greet:
    args:
      - name: msg
        required: true
    cmd: "echo {{.msg}}"
`)

	_, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error for valid module: %v", err)
	}
}

// TestValidateDefaultCmdInputs: схема default-команды проверяется с путём (default).
func TestValidateDefaultCmdInputs(t *testing.T) {
	mod := &Module{
		Name: "meta",
		Command: Command{
			Cmd: cmdText("echo {{.json}}"),
			Flags: []inputs.Spec{
				{Name: "json", Type: "bool", Flag: true},
				{Name: "json", Type: "bool", Flag: true},
				{Name: "scale", Type: "float", Flag: true},
			},
		},
	}

	errs, _ := mod.Validate()
	var got []string
	for _, e := range errs {
		got = append(got, e.Error())
	}
	for _, want := range []string{
		`command "(default)": flag "json": duplicate name "json"`,
		`command "(default)": flag "scale": invalid type "float"`,
	} {
		found := false
		for _, g := range got {
			found = found || strings.Contains(g, want)
		}
		if !found {
			t.Errorf("errors = %q, want one containing %q", got, want)
		}
	}
}

// TestValidateReservedInputNames: зарезервированные имена входов — ошибка
// схемы в команде, вложенной команде и default-команде модуля.
func TestValidateReservedInputNames(t *testing.T) {
	for _, name := range []string{"help", "env", "module_name", "module_root"} {
		a := []inputs.Spec{{Name: name}}
		cases := map[string]*Module{
			"command": {Command: Command{Commands: map[string]*Command{"run": {Cmd: cmdText("x"), Args: a}}}},
			"nested": {Command: Command{Commands: map[string]*Command{"grp": {Commands: map[string]*Command{
				"run": {Cmd: cmdText("x"), Args: a},
			}}}}},
			"default": {Command: Command{Cmd: cmdText("x"), Args: a}},
		}
		for kind, mod := range cases {
			var reserved []string
			errs, _ := mod.Validate()
			for _, e := range errs {
				if strings.Contains(e.Error(), "is reserved") {
					reserved = append(reserved, e.Error())
				}
			}
			if len(reserved) != 1 {
				t.Errorf("%s/%s: errors = %v, want one 'is reserved'", kind, name, errs)
			}
		}
	}
}

func TestValidateSchemaRules(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string // подстрока ожидаемой ошибки; пусто — ошибок быть не должно
	}{
		{"module args without cmd", "args:\n  - name: a\ncommands:\n  x:\n    cmd: echo\n", `command "(default)": args/flags require cmd`},
		{"module flags without cmd", "flags:\n  - name: a\ncommands:\n  x:\n    cmd: echo\n", `command "(default)": args/flags require cmd`},
		{"branch args without cmd", "commands:\n  b:\n    args:\n      - name: a\n    commands:\n      c:\n        cmd: echo\n", `command "b": args/flags require cmd`},
		{"blank cmd", "commands:\n  b:\n    cmd: \"  \"\n", `command "b": must have cmd or commands`},
		{"user command named (default)", "commands:\n  \"(default)\": {}\n", `command "(default)": must have cmd or commands`},
		{"blank default cmd with args", "cmd: \"  \"\nargs:\n  - name: a\ncommands:\n  x:\n    cmd: echo\n", `command "(default)": args/flags require cmd`},
		{"broken cmd template", "commands:\n  b:\n    cmd: 'echo {{.msg'\n", `command "b": cmd: `},
		{"broken default template", "cmd: 'echo {{.msg'\n", `command "(default)": cmd: `},
		{"undeclared input", "commands:\n  b:\n    cmd: 'echo {{.msg}}'\n", `command "b": cmd: `},
		{"unused input", "commands:\n  b:\n    cmd: echo\n    args:\n      - name: x\n", `command "b": cmd: `},
		{"list form cmd", "commands:\n  b:\n    cmd: [echo, '{{.x}}']\n    args:\n      - name: x\n", ""},
		{"broken attach", "attach: '{{.x'\ncmd: echo\n", `attach: invalid template:`},
		{"broken detach", "detach: '{{.x'\ncmd: echo\n", `detach: invalid template:`},
		{"name starts with dash", "commands:\n  -x:\n    cmd: echo\n", `command "-x": invalid command name`},
		{"name with dot", "commands:\n  a.b:\n    cmd: echo\n", `command "a.b": invalid command name`},
		{"name with space", "commands:\n  \"a b\":\n    cmd: echo\n", `command "a b": invalid command name`},
		{"empty name", "commands:\n  \"\":\n    cmd: echo\n", `command "": invalid command name`},
		{"required with default", "commands:\n  b:\n    cmd: 'echo {{.x}}'\n    flags:\n      - name: x\n        required: true\n        default: v\n", `command "b": flag "x": required and default are mutually exclusive`},
		{"int default not int", "commands:\n  b:\n    cmd: 'echo {{.x}}'\n    flags:\n      - name: x\n        type: int\n        default: abc\n", `command "b": flag "x": `},
		{"default cmd bad type", "cmd: 'echo {{.x}}'\nargs:\n  - name: x\n    type: float\n", `command "(default)": arg "x": invalid type "float"`},
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

// TestValidateBuiltinNamesAllowedAsCommands: builtins are recognized only as
// the first CLI word, so list, help and validate are valid command names.
func TestValidateBuiltinNamesAllowedAsCommands(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{"list top level", "commands:\n  list:\n    cmd: echo\n"},
		{"list nested", "commands:\n  a:\n    commands:\n      list:\n        cmd: echo\n"},
		{"help allowed", "commands:\n  help:\n    cmd: echo\n"},
		{"validate allowed", "commands:\n  validate:\n    cmd: echo\n"},
		{"list as arg name allowed", "commands:\n  x:\n    cmd: 'echo {{.list}}'\n    args:\n      - name: list\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAndValidate(t, tc.manifest); len(got) != 0 {
				t.Fatalf("errors = %q, want none", got)
			}
		})
	}
}

func TestValidateOrderIsDeterministic(t *testing.T) {
	mod := &Module{Command: Command{Commands: map[string]*Command{
		"d": {}, "c": {}, "b": {}, "a": {},
	}}}
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
		Attach: "{{.x",
		Command: Command{
			Cmd:      cmdText("echo {{.p}}"),
			Args:     []inputs.Spec{{Name: "p", Type: "float"}},
			Commands: map[string]*Command{"b": {Cmd: cmdText("echo"), Commands: map[string]*Command{"z": {}, "y": {}}}, "a": {}},
		},
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

func TestValidateEmptyCmdWithArgsNoDuplicateErrors(t *testing.T) {
	got := parseAndValidate(t, "args:\n  - name: a\n")
	want := []string{
		"module has no cmd and no commands",
		`command "(default)": args/flags require cmd`,
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}

// TestValidateOldFormat: the old format is rejected with a pointer to the new
// one. params and positional are reported at module and command level; quote,
// .args and .args_list come from the template check.
func TestValidateOldFormat(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{"module params", "cmd: echo\nparams:\n  - name: a\n", `line 2: "params" was replaced by "args"/"flags" (see README)`},
		{"module positional", "cmd: echo\npositional:\n  min: 1\n", `line 2: "positional" was replaced by "args"/"flags" (see README)`},
		{"command params", "commands:\n  b:\n    cmd: echo\n    params:\n      - name: a\n", `line 4: "params" was replaced by "args"/"flags" (see README)`},
		{"nested command positional", "commands:\n  b:\n    commands:\n      c:\n        cmd: echo\n        positional:\n          min: 1\n", `line 6: "positional" was replaced by "args"/"flags" (see README)`},
		{"quote", "commands:\n  b:\n    cmd: 'echo {{quote .x}}'\n    args:\n      - name: x\n", `command "b": cmd: `},
		{"args", "commands:\n  b:\n    cmd: 'echo {{.args}}'\n", `command "b": cmd: `},
		{"args_list", "commands:\n  b:\n    cmd: 'echo {{index .args_list 0}}'\n", `command "b": cmd: `},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAndValidate(t, tc.manifest)
			for _, e := range got {
				if strings.Contains(e, tc.want) {
					if strings.Contains(e, "unknown key") {
						t.Fatalf("error %q must not be an unknown-key error", e)
					}
					return
				}
			}
			t.Fatalf("errors = %q, want one containing %q", got, tc.want)
		})
	}

	t.Run("quote hint", func(t *testing.T) {
		got := parseAndValidate(t, "commands:\n  b:\n    cmd: 'echo {{quote .x}}'\n    args:\n      - name: x\n")
		if len(got) != 1 || !strings.Contains(got[0], "quote was removed: a value is always exactly one argument") {
			t.Fatalf("errors = %q", got)
		}
	})

	t.Run("no unknown-key error for old keys", func(t *testing.T) {
		for _, e := range parseAndValidate(t, "cmd: echo\nparams:\n  - name: a\n") {
			if strings.Contains(e, "unknown key") {
				t.Fatalf("got %q", e)
			}
		}
	})
}

// TestInputRoleFromList: the role of an input (arg or flag) comes from the
// list it is declared in: Validate labels it "arg"/"flag", and the loaded
// specs print as --name for a flag and <name> for an arg in Bind errors.
func TestInputRoleFromList(t *testing.T) {
	manifest := `
commands:
  b:
    args:
      - {name: pod, required: true, type: float}
    flags:
      - {name: tag, required: true, type: float}
    cmd: "kubectl {{.pod}} {{.tag}}"
`
	got := parseAndValidate(t, manifest)
	if len(got) != 2 || !strings.Contains(got[0], `arg "pod"`) || !strings.Contains(got[1], `flag "tag"`) {
		t.Fatalf("errors = %q, want arg \"pod\" then flag \"tag\"", got)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	writeFile(t, p, strings.ReplaceAll(manifest, "type: float", "type: string"))
	mod, err := LoadModule(p, dir)
	if err != nil {
		t.Fatal(err)
	}
	b := mod.Commands["b"]
	if got := b.Args[0].CLIName(); got != "<pod>" {
		t.Errorf("arg CLIName = %q, want <pod>", got)
	}
	if got := b.Flags[0].CLIName(); got != "--tag" {
		t.Errorf("flag CLIName = %q, want --tag", got)
	}
	if _, err := inputs.Bind(b.Args, b.Flags, map[string]any{"pod": "x"}); err == nil || err.Error() != "missing required flag --tag" {
		t.Errorf("Bind err = %v, want missing required flag --tag", err)
	}
	if _, err := inputs.Bind(b.Args, b.Flags, map[string]any{"tag": "x"}); err == nil || err.Error() != "missing argument <pod>" {
		t.Errorf("Bind err = %v, want missing argument <pod>", err)
	}
}

// TestValidateShadowWarning: a node with subcommands and a positional string
// arg warns that a subcommand name cannot be passed as a value; int args do
// not.
func TestValidateShadowWarning(t *testing.T) {
	db := func(stepsType string) string {
		return `
commands:
  migrate:
    args:
      - name: steps
        type: ` + stepsType + `
    cmd: "migrate {{.steps}}"
    commands:
      up: {cmd: "migrate up"}
      down: {cmd: "migrate down"}
`
	}
	dir := t.TempDir()
	load := func(manifest string) []string {
		p := filepath.Join(dir, "db.yaml")
		writeFile(t, p, manifest)
		mod, err := ParseModule(p, dir)
		if err != nil {
			t.Fatal(err)
		}
		errs, warns := mod.Validate()
		if len(errs) != 0 {
			t.Fatalf("errors = %v", errs)
		}
		return warns
	}

	want := `command "migrate": words down, up are subcommands; pass such a value after --`
	if got := load(db("string")); len(got) != 1 || got[0] != want {
		t.Fatalf("string steps: warnings = %q, want [%s]", got, want)
	}
	if got := load(db("int")); len(got) != 0 {
		t.Fatalf("int steps: warnings = %q, want none", got)
	}
}

func TestShadowWarnings(t *testing.T) {
	sub := map[string]*Command{"up": {Cmd: cmdText("x")}, "down": {Cmd: cmdText("x")}}
	tests := []struct {
		name string
		cmd  *Command
		want []string
	}{
		{"string arg", &Command{Args: []inputs.Spec{{Name: "a"}}, Commands: sub},
			[]string{`command "db migrate": words down, up are subcommands; pass such a value after --`}},
		{"variadic string arg", &Command{Args: []inputs.Spec{{Name: "a", Type: "string", Variadic: true}}, Commands: sub},
			[]string{`command "db migrate": words down, up are subcommands; pass such a value after --`}},
		{"rest arg", &Command{Args: []inputs.Spec{{Name: "a", Rest: true}}, Commands: sub},
			[]string{`command "db migrate": words down, up are subcommands; pass such a value after --`}},
		{"only int args", &Command{Args: []inputs.Spec{{Name: "a", Type: "int"}}, Commands: sub}, nil},
		{"no args", &Command{Commands: sub}, nil},
		{"no subcommands", &Command{Args: []inputs.Spec{{Name: "a"}}}, nil},
		{"flags only", &Command{Flags: []inputs.Spec{{Name: "a", Flag: true}}, Commands: sub}, nil},
		{"recursive", &Command{Commands: map[string]*Command{
			"up": {Args: []inputs.Spec{{Name: "n"}}, Commands: map[string]*Command{"now": {}}},
		}}, []string{`command "db migrate up": words now are subcommands; pass such a value after --`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShadowWarnings("db migrate", tt.cmd)
			if len(got) != len(tt.want) {
				t.Fatalf("ShadowWarnings = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ShadowWarnings[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
