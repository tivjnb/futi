package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/completion"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
)

// helpTestSource создаёт GlobalSource с модулями для тестов help:
//
//	demo.yaml    — команды greet (leaf с params), db (ветка up/down), help (user-defined)
//	plain.yaml   — модуль без user-defined help
func helpTestSource(t *testing.T) *core.Runtime {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "demo.yaml"), `
name: demo
description: Feature showcase
commands:
  greet:
    description: Greet someone
    params:
      - name: name
        type: string
        required: true
    cmd: "echo Hello, {{.name}}"
  db:
    description: Nested command demo
    commands:
      up:
        cmd: "echo migrate up"
      down:
        cmd: "echo migrate down"
  help:
    description: Custom help
    cmd: "echo CUSTOM"
`)
	writeFile(t, filepath.Join(dir, "plain.yaml"), `
name: plain
description: Plain module
commands:
  run:
    cmd: "echo running"
`)
	return testRuntime(t, core.Options{ModulesDir: dir})
}

// helpArgs — helpRequest для позиционного пути без флагов и args.
func helpArgs(positional ...string) *helpRequest {
	return &helpRequest{positional: positional}
}

// testRuntime builds a Runtime with the CLI exec policy for tests.
func testRuntime(t *testing.T, opts core.Options) *core.Runtime {
	t.Helper()
	opts.Exec = core.CLIExecPolicy
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

// runHelpFor запускает runHelp с раздельными буферами stdout/stderr и
// возвращает код выхода.
func runHelpFor(t *testing.T, rt *core.Runtime, req *helpRequest) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code, _ = runHelp(context.Background(), nil, &out, &errOut, rt, req)
	return code, out.String(), errOut.String()
}

func TestRunHelpModule(t *testing.T) {
	rt := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("plain"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	s := stdout
	if !strings.Contains(s, "Plain module") || !strings.Contains(s, "run") {
		t.Fatalf("expected module help, got:\n%s", s)
	}
}

func TestRunHelpLeafCommand(t *testing.T) {
	rt := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("demo", "greet"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	s := stdout
	if !strings.Contains(s, "Greet someone") || !strings.Contains(s, "--name") || !strings.Contains(s, "required") {
		t.Fatalf("expected command help with params, got:\n%s", s)
	}
}

func TestRunHelpBranch(t *testing.T) {
	rt := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("demo", "db"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	s := stdout
	if !strings.Contains(s, "Nested command demo") || !strings.Contains(s, "up") || !strings.Contains(s, "down") {
		t.Fatalf("expected branch help with subcommands, got:\n%s", s)
	}
}

func TestRunHelpUserDefinedWins(t *testing.T) {
	rt := helpTestSource(t)
	// demo имеет команду help → user-defined help выигрывает (выполняется).
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("demo"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "CUSTOM") {
		t.Fatalf("expected user-defined help to run, got:\n%s", stdout)
	}
}

func TestRunHelpUserDefinedSubcommandWins(t *testing.T) {
	rt := helpTestSource(t)
	// demo.greet не имеет subcommand help → системная справка команды.
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("demo", "greet"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "CUSTOM") {
		t.Fatalf("unexpected user-defined help for greet: %s", stdout)
	}
}

func TestRunHelpRoot(t *testing.T) {
	// runHelp с пустым positional печатает usage в stdout (printUsage).
	rt := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs())
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "USAGE") {
		t.Fatalf("usage не в stdout:\n%s", stdout)
	}
}

func TestRunHelpNotFound(t *testing.T) {
	rt := helpTestSource(t)
	code, _, stderr := runHelpFor(t, rt, helpArgs("demo", "nonexistent"))
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "nonexistent") {
		t.Fatalf("expected command name in error, got: %v", stderr)
	}
}

func TestParseArgsHelpDetection(t *testing.T) {
	_, err := parseArgs([]string{"demo", "greet", "help"})
	if err == nil {
		t.Fatal("expected helpRequest")
	}
	var hr *helpRequest
	if !errors.As(err, &hr) {
		t.Fatalf("expected helpRequest, got %T", err)
	}
	if len(hr.positional) != 2 || hr.positional[0] != "demo" || hr.positional[1] != "greet" {
		t.Fatalf("unexpected positional: %v", hr.positional)
	}
}

// Usage перечисляет каждое зарезервированное слово корня в кавычках.
func TestUsageListsAllReservedWords(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()
	for _, w := range completion.ReservedNames {
		if !strings.Contains(usage, `"`+w+`"`) {
			t.Errorf("usage does not mention reserved word %q", w)
		}
	}
	if strings.Contains(usage, "hybrid mode") {
		t.Errorf("usage still mentions hybrid mode")
	}
}

// userHelpSource — модули с пользовательской командой help:
//
//	m    — required-параметр x, печатает x и args в stdout, "ERRTEXT" в stderr
//	bad  — help завершается с кодом 3
//	plain — без пользовательской help
func userHelpSource(t *testing.T) *core.Runtime {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "m.yaml"), `
name: m
commands:
  help:
    description: Custom help text
    params:
      - name: x
        type: string
        required: true
    cmd: "sh -c 'echo x=$1 args=$2; echo ERRTEXT >&2' sh {{quote .x}} {{quote .args}}"
`)
	writeFile(t, filepath.Join(dir, "bad.yaml"), `
name: bad
commands:
  help:
    cmd: "sh -c 'exit 3'"
`)
	writeFile(t, filepath.Join(dir, "plain.yaml"), `
name: plain
description: Plain module
commands:
  run:
    cmd: "echo running"
`)
	return testRuntime(t, core.Options{ModulesDir: dir})
}

func TestRunHelpUserCommandGetsFlagsAndArgs(t *testing.T) {
	rt := userHelpSource(t)
	req := &helpRequest{
		positional: []string{"m"},
		flags:      []string{"--x", "1"},
		argsList:   []string{"a", "b"},
	}
	code, stdout, stderr := runHelpFor(t, rt, req)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "x=1 args=a b") {
		t.Fatalf("stdout = %q, want it to contain %q", stdout, "x=1 args=a b")
	}
}

func TestRunHelpUserCommandStderrSeparate(t *testing.T) {
	rt := userHelpSource(t)
	req := &helpRequest{positional: []string{"m"}, flags: []string{"--x", "1"}}
	_, stdout, stderr := runHelpFor(t, rt, req)
	if strings.Contains(stdout, "ERRTEXT") {
		t.Fatalf("stderr команды попал в stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "ERRTEXT") {
		t.Fatalf("stderr = %q, want it to contain ERRTEXT", stderr)
	}
}

func TestRunHelpUserCommandExitCode(t *testing.T) {
	rt := userHelpSource(t)
	var out, errOut bytes.Buffer
	code, err := runHelp(context.Background(), nil, &out, &errOut, rt, helpArgs("bad"))
	if code != 3 {
		t.Fatalf("code = %d, want 3 (stderr: %s)", code, errOut.String())
	}
	if err == nil {
		t.Fatal("ожидалась ошибка запуска для сигнального пути вызывающего")
	}
}

func TestRunHelpUserCommandMissingRequiredParam(t *testing.T) {
	rt := userHelpSource(t)
	code, _, stderr := runHelpFor(t, rt, helpArgs("m"))
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "required") {
		t.Fatalf("stderr = %q, want it to mention required", stderr)
	}
}

func TestRunHelpUserCommandHelpFlagShowsItsHelp(t *testing.T) {
	rt := userHelpSource(t)
	req := &helpRequest{positional: []string{"m"}, flags: []string{"--help"}}
	code, stdout, stderr := runHelpFor(t, rt, req)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	// Исполнение дало бы ERRTEXT в stderr (или ошибку required): stderr пуст.
	if !strings.Contains(stdout, "Custom help text") || stderr != "" {
		t.Fatalf("stdout = %q, stderr = %q, want system help of the help command, not its execution", stdout, stderr)
	}
}

func TestRunHelpSystemIgnoresFlags(t *testing.T) {
	rt := userHelpSource(t)
	req := &helpRequest{positional: []string{"plain"}, flags: []string{"--x", "1"}, argsList: []string{"a"}}
	code, stdout, stderr := runHelpFor(t, rt, req)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Plain module") {
		t.Fatalf("stdout = %q, want system help of module", stdout)
	}
}

func TestParseArgsHelpRequestKeepsFlagsAndArgs(t *testing.T) {
	_, err := parseArgs([]string{"m", "help", "--x", "1", "--", "a", "b"})
	var hr *helpRequest
	if !errors.As(err, &hr) {
		t.Fatalf("expected helpRequest, got %v", err)
	}
	if strings.Join(hr.flags, " ") != "--x 1" || strings.Join(hr.argsList, ",") != "a,b" {
		t.Fatalf("flags=%v argsList=%v", hr.flags, hr.argsList)
	}
}

// `futi help` при модуле help с default cmd: namespace="help", command="".
func TestRunHelpUserModuleNamedHelp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "help.yaml"), `
name: help
cmd: "echo MODULEHELP"
`)
	code, stdout, stderr := runHelpFor(t, testRuntime(t, core.Options{ModulesDir: dir}), helpArgs())
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "MODULEHELP") {
		t.Fatalf("stdout = %q, want default cmd of module help", stdout)
	}
}

// `demo.greet help` должен вести себя как `demo greet help`: сегмент пути с
// точкой делится на namespace и команду.
func TestParseArgsHelpDotNamespaceMatchesSpacedPath(t *testing.T) {
	rt := helpTestSource(t)
	_, err := parseArgs([]string{"demo.greet", "help"})
	var req *helpRequest
	if !errors.As(err, &req) {
		t.Fatalf("err = %v, want helpRequest", err)
	}
	code, stdout, stderr := runHelpFor(t, rt, req)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Greet someone") || !strings.Contains(stdout, "--name") {
		t.Fatalf("expected help of command greet, got:\n%s", stdout)
	}
}

// staleCacheSource создаёт глобальный модуль stale.yaml, прогревает кэш через
// быстрый LoadModule и затем переписывает манифест на диске. Возвращает source,
// кэш с устаревшей записью.
func staleCacheSource(t *testing.T, before, after string) (discovery.Source, *modulecache.Cache) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.yaml")
	writeFile(t, path, before)
	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	manifest, root, err := src.Resolve("stale")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := cache.LoadModule(manifest, root); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	writeFile(t, path, after)
	return src, cache
}

func TestRunSystemHelpReloadsChangedManifest(t *testing.T) {
	src, cache := staleCacheSource(t, `
name: stale
description: Old description
commands:
  run:
    cmd: "echo run"
`, `
name: stale
description: New description
commands:
  run:
    cmd: "echo run"
`)
	var out bytes.Buffer
	if err := runSystemHelp(&out, src, cache, []string{"stale"}); err != nil {
		t.Fatalf("runSystemHelp: %v", err)
	}
	if !strings.Contains(out.String(), "New description") {
		t.Errorf("help output = %q, want it to contain %q", out.String(), "New description")
	}
}

func TestIsExecutableHelpReloadsChangedManifest(t *testing.T) {
	src, cache := staleCacheSource(t, `
name: stale
commands:
  run:
    cmd: "echo run"
`, `
name: stale
commands:
  run:
    cmd: "echo run"
  help:
    cmd: "echo CUSTOM"
`)
	if _, ok := core.ExecutableHelpIn(src, cache, []string{"stale", "help"}); !ok {
		t.Error("ExecutableHelpIn executable = false, want true (help добавлена на диске)")
	}
}

// TestRunHelpDecision pins which path runHelp takes (user-defined help vs
// system help) and what each path prints.
func TestRunHelpDecision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "leaf.yaml"), `
name: leaf
commands:
  help:
    params:
      - name: x
        type: string
    cmd: "echo leafhelp x={{.x}} args={{quote .args}}"
`)
	writeFile(t, filepath.Join(dir, "branch.yaml"), `
name: branch
description: Branch module
commands:
  help:
    commands:
      sub:
        cmd: "echo sub"
`)
	writeFile(t, filepath.Join(dir, "plain.yaml"), `
name: plain
description: Plain module
commands:
  run:
    cmd: "echo running"
`)
	writeFile(t, filepath.Join(dir, "x", "help.yaml"), `
name: xhelp
cmd: "echo xhelp"
`)
	writeFile(t, filepath.Join(dir, "bad.yaml"), "commands: [")
	writeFile(t, filepath.Join(dir, "ex.yaml"), `
name: ex
commands:
  help:
    cmd: "sh -c 'exit 3'"
`)
	writeFile(t, filepath.Join(dir, "c", "help.yaml"), `
name: chelp
cmd: "echo a"
`)
	writeFile(t, filepath.Join(dir, "c", "help", "config.yaml"), `
name: chelp2
cmd: "echo b"
`)
	rt := testRuntime(t, core.Options{ModulesDir: dir})

	tests := []struct {
		name       string
		req        *helpRequest
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"executable leaf binds flag", &helpRequest{positional: []string{"leaf"}, flags: []string{"--x", "1"}}, 0, "leafhelp x=1 args=\n", ""},
		{"branch without cmd gives system help", helpArgs("branch"), 0, "branch — Branch module\ncommands:\n  help\n", ""},
		{"module without help gives system help", helpArgs("plain"), 0, "plain — Plain module\ncommands:\n  run\n", ""},
		{"positional x resolves to module x.help", helpArgs("x"), 0, "xhelp\n", ""},
		{"broken yaml", helpArgs("bad"), 1, "", "help: execmodule: parse manifest \"" + filepath.Join(dir, "bad.yaml") + "\": yaml: line 1: did not find expected node content\n"},
		{"unknown path with dry-run is plain not-found", &helpRequest{positional: []string{"nope"}, dryRun: true}, 1, "", "help: module not found for \"nope\"\n"},
		{"known module without help with dry-run is rejected", &helpRequest{positional: []string{"plain"}, dryRun: true}, 1, "", "futi: --dry-run applies only to module commands\n"},
		{"executable help gets args", &helpRequest{positional: []string{"leaf"}, argsList: []string{"a", "b"}}, 0, "leafhelp x= args=a b\n", ""},
		{"executable help with dry-run prints argv", &helpRequest{positional: []string{"leaf"}, flags: []string{"--x", "1"}, dryRun: true}, 0, "echo leafhelp x=1 args=\n", ""},
		{"executable help exit code", helpArgs("ex"), 3, "", "run: executor: run \"sh\": exit status 3\n"},
		{"yaml vs config.yaml conflict on help", helpArgs("c"), 0, "┌────────┬──────┬──────────────────────────────────────────┐\n│  TYPE  │ NAME │               DESCRIPTION                │\n├────────┼──────┼──────────────────────────────────────────┤\n│ module │ help │ conflict: help.yaml and help/config.yaml │\n└────────┴──────┴──────────────────────────────────────────┘\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runHelpFor(t, rt, tc.req)
			if code != tc.wantCode || stdout != tc.wantStdout || stderr != tc.wantStderr {
				t.Errorf("got (%d, %q, %q), want (%d, %q, %q)", code, stdout, stderr, tc.wantCode, tc.wantStdout, tc.wantStderr)
			}
		})
	}
}
