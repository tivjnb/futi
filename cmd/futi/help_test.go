package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/completion"
	"github.com/tivjnb/futi/internal/discovery"
)

// helpTestSource создаёт GlobalSource с модулями для тестов help:
//
//	demo.yaml    — команды greet (leaf с params), db (ветка up/down), help (user-defined)
//	plain.yaml   — модуль без user-defined help
func helpTestSource(t *testing.T) discovery.Source {
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
	return discovery.NewGlobalSource(dir)
}

// helpArgs — helpRequest для позиционного пути без флагов и args.
func helpArgs(positional ...string) *helpRequest {
	return &helpRequest{positional: positional}
}

// runHelpFor запускает runHelp с раздельными буферами stdout/stderr и
// возвращает код выхода.
func runHelpFor(t *testing.T, src discovery.Source, req *helpRequest) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code, _ = runHelp(context.Background(), &out, &errOut, src, nil, req)
	return code, out.String(), errOut.String()
}

func TestRunHelpModule(t *testing.T) {
	src := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, src, helpArgs("plain"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	s := stdout
	if !strings.Contains(s, "Plain module") || !strings.Contains(s, "run") {
		t.Fatalf("expected module help, got:\n%s", s)
	}
}

func TestRunHelpLeafCommand(t *testing.T) {
	src := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, src, helpArgs("demo", "greet"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	s := stdout
	if !strings.Contains(s, "Greet someone") || !strings.Contains(s, "--name") || !strings.Contains(s, "required") {
		t.Fatalf("expected command help with params, got:\n%s", s)
	}
}

func TestRunHelpBranch(t *testing.T) {
	src := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, src, helpArgs("demo", "db"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	s := stdout
	if !strings.Contains(s, "Nested command demo") || !strings.Contains(s, "up") || !strings.Contains(s, "down") {
		t.Fatalf("expected branch help with subcommands, got:\n%s", s)
	}
}

func TestRunHelpUserDefinedWins(t *testing.T) {
	src := helpTestSource(t)
	// demo имеет команду help → user-defined help выигрывает (выполняется).
	code, stdout, stderr := runHelpFor(t, src, helpArgs("demo"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "CUSTOM") {
		t.Fatalf("expected user-defined help to run, got:\n%s", stdout)
	}
}

func TestRunHelpUserDefinedSubcommandWins(t *testing.T) {
	src := helpTestSource(t)
	// demo.greet не имеет subcommand help → системная справка команды.
	code, stdout, stderr := runHelpFor(t, src, helpArgs("demo", "greet"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "CUSTOM") {
		t.Fatalf("unexpected user-defined help for greet: %s", stdout)
	}
}

func TestRunHelpRoot(t *testing.T) {
	// runHelp с пустым positional печатает usage в stdout (printUsage).
	src := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, src, helpArgs())
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "USAGE") {
		t.Fatalf("usage не в stdout:\n%s", stdout)
	}
}

func TestRunHelpNotFound(t *testing.T) {
	src := helpTestSource(t)
	code, _, stderr := runHelpFor(t, src, helpArgs("demo", "nonexistent"))
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
func userHelpSource(t *testing.T) discovery.Source {
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
	return discovery.NewGlobalSource(dir)
}

func TestRunHelpUserCommandGetsFlagsAndArgs(t *testing.T) {
	src := userHelpSource(t)
	req := &helpRequest{
		positional: []string{"m"},
		flags:      []string{"--x", "1"},
		args:       "a b",
		argsList:   []string{"a", "b"},
	}
	code, stdout, stderr := runHelpFor(t, src, req)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "x=1 args=a b") {
		t.Fatalf("stdout = %q, want it to contain %q", stdout, "x=1 args=a b")
	}
}

func TestRunHelpUserCommandStderrSeparate(t *testing.T) {
	src := userHelpSource(t)
	req := &helpRequest{positional: []string{"m"}, flags: []string{"--x", "1"}}
	_, stdout, stderr := runHelpFor(t, src, req)
	if strings.Contains(stdout, "ERRTEXT") {
		t.Fatalf("stderr команды попал в stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "ERRTEXT") {
		t.Fatalf("stderr = %q, want it to contain ERRTEXT", stderr)
	}
}

func TestRunHelpUserCommandExitCode(t *testing.T) {
	src := userHelpSource(t)
	var out, errOut bytes.Buffer
	code, err := runHelp(context.Background(), &out, &errOut, src, nil, helpArgs("bad"))
	if code != 3 {
		t.Fatalf("code = %d, want 3 (stderr: %s)", code, errOut.String())
	}
	if err == nil {
		t.Fatal("ожидалась ошибка запуска для сигнального пути вызывающего")
	}
}

func TestRunHelpUserCommandMissingRequiredParam(t *testing.T) {
	src := userHelpSource(t)
	code, _, stderr := runHelpFor(t, src, helpArgs("m"))
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "required") {
		t.Fatalf("stderr = %q, want it to mention required", stderr)
	}
}

func TestRunHelpUserCommandHelpFlagShowsItsHelp(t *testing.T) {
	src := userHelpSource(t)
	req := &helpRequest{positional: []string{"m"}, flags: []string{"--help"}}
	code, stdout, stderr := runHelpFor(t, src, req)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	// Исполнение дало бы ERRTEXT в stderr (или ошибку required): stderr пуст.
	if !strings.Contains(stdout, "Custom help text") || stderr != "" {
		t.Fatalf("stdout = %q, stderr = %q, want system help of the help command, not its execution", stdout, stderr)
	}
}

func TestRunHelpSystemIgnoresFlags(t *testing.T) {
	src := userHelpSource(t)
	req := &helpRequest{positional: []string{"plain"}, flags: []string{"--x", "1"}, args: "a", argsList: []string{"a"}}
	code, stdout, stderr := runHelpFor(t, src, req)
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
	if strings.Join(hr.flags, " ") != "--x 1" || hr.args != "a b" || strings.Join(hr.argsList, ",") != "a,b" {
		t.Fatalf("flags=%v args=%q argsList=%v", hr.flags, hr.args, hr.argsList)
	}
}

// `futi help` при модуле help с default cmd: namespace="help", command="".
func TestRunHelpUserModuleNamedHelp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "help.yaml"), `
name: help
cmd: "echo MODULEHELP"
`)
	code, stdout, stderr := runHelpFor(t, discovery.NewGlobalSource(dir), helpArgs())
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "MODULEHELP") {
		t.Fatalf("stdout = %q, want default cmd of module help", stdout)
	}
}
