package execmodule

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/inputs"
	"gopkg.in/yaml.v3"
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
    flags:
      - name: tag
        type: string
        required: true
        short: t
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
	if !reflect.DeepEqual(ps.Cmd, CmdSpec{Text: "docker ps"}) {
		t.Fatalf("unexpected ps cmd: %+v", ps.Cmd)
	}
	if ps.Description != "List containers" {
		t.Fatalf("unexpected ps description: %q", ps.Description)
	}

	build, ok := mod.Commands["build"]
	if !ok {
		t.Fatal("command 'build' not found")
	}
	if len(build.Flags) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(build.Flags))
	}
	if f := build.Flags[0]; f.Name != "tag" || !f.Required || f.Short != "t" || !f.Flag {
		t.Fatalf("unexpected flag 0: %+v", f)
	}
	if f := build.Flags[1]; f.Name != "file" || f.Default == nil || *f.Default != "Dockerfile" || !f.Flag {
		t.Fatalf("unexpected flag 1: %+v", f)
	}
}

func TestLoadModuleUnknownCommandNotInModule(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "one.yaml")
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
cmd: "echo {{.words}}"
args:
  - name: words
    variadic: true
commands:
  upper:
    cmd: "tr a-z A-Z"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(mod.Cmd, CmdSpec{Text: "echo {{.words}}"}) {
		t.Fatalf("unexpected default cmd: %+v", mod.Cmd)
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
	if !mod.Cmd.IsZero() {
		t.Fatalf("expected empty default cmd, got %+v", mod.Cmd)
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
        args:
          - name: name
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
	if up.Cmd.Text != "echo migrating up" {
		t.Fatalf("unexpected up cmd: %+v", up.Cmd)
	}
	if up.Description != "Run migrations up" {
		t.Fatalf("unexpected up description: %q", up.Description)
	}

	create, ok := migrate.Commands["create"]
	if !ok {
		t.Fatal("subcommand 'create' not found")
	}
	if len(create.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(create.Args))
	}
	if create.Args[0].Name != "name" || !create.Args[0].Required || create.Args[0].Flag {
		t.Fatalf("unexpected arg: %+v", create.Args[0])
	}

	status, ok := mod.Commands["status"]
	if !ok {
		t.Fatal("command 'status' not found")
	}
	if status.Cmd.Text != "echo status" {
		t.Fatalf("unexpected status cmd: %+v", status.Cmd)
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
	if up.Commands["force"].Cmd.Text != "echo force" {
		t.Fatal("nested subcommand 'force' not parsed correctly")
	}
	if up.Commands["safe"].Cmd.Text != "echo safe" {
		t.Fatal("nested subcommand 'safe' not parsed correctly")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}

func TestLoadModuleParsesInputDescription(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "mod.yaml")
	writeFile(t, manifestPath, `
name: mod
commands:
  run:
    args:
      - name: tag
        required: true
        description: Image tag
    cmd: "echo {{.tag}}"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	run := mod.Commands["run"]
	if run.Args[0].Description != "Image tag" {
		t.Fatalf("unexpected arg description: %q", run.Args[0].Description)
	}
}

func TestLoadModuleDefaultCmdInputs(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "meta.yaml")
	writeFile(t, manifestPath, `
name: meta
cmd: "echo {{.sep}} {{.words}}"
args:
  - name: words
    required: true
    variadic: true
flags:
  - name: sep
    default: "|"
commands:
  show:
    cmd: "echo show"
`)

	mod, err := LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mod.Args) != 1 || mod.Args[0].Name != "words" || !mod.Args[0].Variadic {
		t.Fatalf("unexpected module args: %+v", mod.Args)
	}
	if len(mod.Flags) != 1 || mod.Flags[0].Name != "sep" || *mod.Flags[0].Default != "|" {
		t.Fatalf("unexpected module flags: %+v", mod.Flags)
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
		if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
			t.Fatalf("ParseModule(файл > 1 МиБ): err = %v, want ошибка с \"exceeds limit\"", err)
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
    args:
      - name: tag
        requird: true
`)
	want := []string{
		`line 2: unknown key "comands"`,
		`line 9: unknown key "requird" in commands.build.args[0]`,
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

// scenarioModules are the modules of the scenario corpus (k8s, files, docker,
// echo, git, db); every one must load without errors and warnings.
var scenarioModules = map[string]string{
	"k8s": `
name: k8s
commands:
  logs:
    description: Show pod logs
    args:
      - name: pod
        required: true
        description: Pod name
    flags:
      - name: follow
        type: bool
        short: f
      - name: tail
        type: int
        default: "100"
    cmd: "kubectl logs {{if .follow}}-f{{end}} --tail={{.tail}} -- {{.pod}}"
`,
	"files": `
name: files
commands:
  copy:
    args:
      - {name: src, required: true, variadic: true}
      - {name: dst, required: true}
    flags:
      - {name: force, type: bool}
    cmd: "cp {{if .force}}-f{{end}} -- {{.src}} {{.dst}}"
  grep:
    args:
      - {name: pattern, required: true}
      - {name: paths, variadic: true}
    flags:
      - {name: ignore_case, type: bool, short: i}
    cmd: "grep {{if .ignore_case}}-i{{end}} -- {{.pattern}} {{.paths}}"
`,
	"docker": `
name: docker
cmd: docker ps
commands:
  build:
    args:
      - {name: context, default: "."}
    flags:
      - {name: tag, required: true, short: t}
      - {name: file, default: Dockerfile}
    cmd: [docker, build, -t, "{{.tag}}", -f, "{{.file}}", "{{.context}}"]
  run:
    args:
      - {name: image, required: true}
      - {name: docker_args, rest: true}
    cmd: "docker run {{.image}} {{.docker_args}}"
`,
	"echo": `
name: echo
cmd: "echo {{.words}}"
args:
  - {name: words, variadic: true}
`,
	"git": `
name: git
cmd: git status
commands:
  commit:
    flags:
      - {name: message, required: true, short: m}
      - {name: amend, type: bool}
    cmd: "git commit -m {{.message}} {{if .amend}}--amend{{end}}"
  help:
    args:
      - {name: topic}
    cmd: "git help {{.topic}}"
`,
	"db": `
name: db
commands:
  migrate:
    args:
      - {name: steps, type: int}
    cmd: migrate {{.steps}}
    commands:
      up:
        cmd: migrate up
      down:
        cmd: migrate down
`,
}

func TestParseManifestInputs(t *testing.T) {
	load := func(t *testing.T, name string) *Module {
		t.Helper()
		mod, err := ParseManifest([]byte(scenarioModules[name]), name+".yaml", "/m")
		if err != nil {
			t.Fatalf("ParseManifest(%s): %v", name, err)
		}
		errs, warns := mod.Validate()
		if len(errs) != 0 || len(warns) != 0 {
			t.Fatalf("%s: errors=%v warnings=%q", name, errs, warns)
		}
		return mod
	}

	t.Run("k8s", func(t *testing.T) {
		logs := load(t, "k8s").Commands["logs"]
		if logs.Description != "Show pod logs" || logs.Cmd.IsList {
			t.Errorf("logs = %+v", logs)
		}
		wantArgs := []inputs.Spec{{Name: "pod", Required: true, Description: "Pod name"}}
		if !reflect.DeepEqual(logs.Args, wantArgs) {
			t.Errorf("Args = %+v, want %+v", logs.Args, wantArgs)
		}
		hundred := "100"
		wantFlags := []inputs.Spec{
			{Name: "follow", Type: "bool", Short: "f", Flag: true},
			{Name: "tail", Type: "int", Default: &hundred, Flag: true},
		}
		if !reflect.DeepEqual(logs.Flags, wantFlags) {
			t.Errorf("Flags = %+v, want %+v", logs.Flags, wantFlags)
		}
	})

	t.Run("files", func(t *testing.T) {
		mod := load(t, "files")
		copyArgs := mod.Commands["copy"].Args
		if !copyArgs[0].Variadic || !copyArgs[0].Required || copyArgs[1].Variadic {
			t.Errorf("copy args = %+v", copyArgs)
		}
		if f := mod.Commands["grep"].Flags[0]; f.Name != "ignore_case" || f.Short != "i" || f.Type != "bool" {
			t.Errorf("grep flag = %+v", f)
		}
	})

	t.Run("docker", func(t *testing.T) {
		mod := load(t, "docker")
		if mod.Cmd.Text != "docker ps" || mod.Cmd.IsList {
			t.Errorf("module cmd = %+v", mod.Cmd)
		}
		build := mod.Commands["build"]
		wantCmd := CmdSpec{List: []string{"docker", "build", "-t", "{{.tag}}", "-f", "{{.file}}", "{{.context}}"}, IsList: true}
		if !reflect.DeepEqual(build.Cmd, wantCmd) {
			t.Errorf("build cmd = %+v, want %+v", build.Cmd, wantCmd)
		}
		if d := build.Args[0].Default; d == nil || *d != "." {
			t.Errorf("context default = %v", d)
		}
		run := mod.Commands["run"]
		if !run.Args[1].Rest || run.Args[1].Name != "docker_args" {
			t.Errorf("run args = %+v", run.Args)
		}
	})

	t.Run("echo", func(t *testing.T) {
		mod := load(t, "echo")
		if len(mod.Args) != 1 || !mod.Args[0].Variadic || len(mod.Commands) != 0 {
			t.Errorf("echo = %+v", mod.Command)
		}
	})

	t.Run("git", func(t *testing.T) {
		mod := load(t, "git")
		if f := mod.Commands["commit"].Flags[0]; f.Name != "message" || !f.Required || f.Short != "m" {
			t.Errorf("commit flag = %+v", f)
		}
		if _, ok := mod.Commands["help"]; !ok {
			t.Error("user command help is missing")
		}
	})

	t.Run("db", func(t *testing.T) {
		migrate := load(t, "db").Commands["migrate"]
		if migrate.Args[0].Type != "int" || len(migrate.Commands) != 2 || migrate.Cmd.Text != "migrate {{.steps}}" {
			t.Errorf("migrate = %+v", migrate)
		}
	})
}

func TestCmdSpecUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want CmdSpec
		err  string
	}{
		{"string", `cmd: echo hi`, CmdSpec{Text: "echo hi"}, ""},
		{"list", `cmd: [echo, "a b", ""]`, CmdSpec{List: []string{"echo", "a b", ""}, IsList: true}, ""},
		{"empty list", `cmd: []`, CmdSpec{List: []string{}, IsList: true}, ""},
		{"number", `cmd: 5`, CmdSpec{Text: "5"}, ""},
		{"mapping", "cmd:\n  a: b\n", CmdSpec{}, "cmd must be a string or a list of strings"},
		{"nested list", "cmd:\n  - [a, b]\n", CmdSpec{}, "cmd must be a string or a list of strings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc struct {
				Cmd CmdSpec `yaml:"cmd"`
			}
			err := yaml.Unmarshal([]byte(tt.yaml), &doc)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(doc.Cmd, tt.want) {
				t.Fatalf("Cmd = %+v, want %+v", doc.Cmd, tt.want)
			}
		})
	}
}

func TestCmdSpecIsZeroAndDisplay(t *testing.T) {
	zero := []CmdSpec{{}, {Text: "  \t"}, {IsList: true}, {List: []string{}, IsList: true}}
	for _, c := range zero {
		if !c.IsZero() {
			t.Errorf("%+v.IsZero() = false, want true", c)
		}
	}
	nonZero := []CmdSpec{{Text: "echo"}, {List: []string{""}, IsList: true}}
	for _, c := range nonZero {
		if c.IsZero() {
			t.Errorf("%+v.IsZero() = true, want false", c)
		}
	}

	display := []struct {
		c    CmdSpec
		want string
	}{
		{CmdSpec{Text: "echo {{.x}}"}, "echo {{.x}}"},
		{CmdSpec{List: []string{"a", "b c"}, IsList: true}, `[a, "b c"]`},
		{CmdSpec{List: []string{"x", "", "y,z"}, IsList: true}, `[x, "", "y,z"]`},
	}
	for _, tt := range display {
		if got := tt.c.Display(); got != tt.want {
			t.Errorf("Display(%+v) = %q, want %q", tt.c, got, tt.want)
		}
	}
}

func TestCmdSpecTemplateForms(t *testing.T) {
	str, err := CmdSpec{Text: `echo "a b"`}.Template()
	if err != nil {
		t.Fatal(err)
	}
	args, err := str.Eval(nil, argvtmpl.Values{})
	if err != nil || !reflect.DeepEqual(argvtmpl.Argv(args), []string{"echo", "a b"}) {
		t.Fatalf("string form: argv = %q, err = %v", argvtmpl.Argv(args), err)
	}

	list, err := CmdSpec{List: []string{"echo", `"a b"`}, IsList: true}.Template()
	if err != nil {
		t.Fatal(err)
	}
	args, err = list.Eval(nil, argvtmpl.Values{})
	if err != nil || !reflect.DeepEqual(argvtmpl.Argv(args), []string{"echo", `"a b"`}) {
		t.Fatalf("list form: argv = %q, err = %v (quotes are literal in a list)", argvtmpl.Argv(args), err)
	}
}
