package main

import (
	"bytes"
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

func TestRunHelpModule(t *testing.T) {
	src := helpTestSource(t)
	var out bytes.Buffer
	if err := runHelp(&out, src, nil, []string{"plain"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Plain module") || !strings.Contains(s, "run") {
		t.Fatalf("expected module help, got:\n%s", s)
	}
}

func TestRunHelpLeafCommand(t *testing.T) {
	src := helpTestSource(t)
	var out bytes.Buffer
	if err := runHelp(&out, src, nil, []string{"demo", "greet"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Greet someone") || !strings.Contains(s, "--name") || !strings.Contains(s, "required") {
		t.Fatalf("expected command help with params, got:\n%s", s)
	}
}

func TestRunHelpBranch(t *testing.T) {
	src := helpTestSource(t)
	var out bytes.Buffer
	if err := runHelp(&out, src, nil, []string{"demo", "db"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Nested command demo") || !strings.Contains(s, "up") || !strings.Contains(s, "down") {
		t.Fatalf("expected branch help with subcommands, got:\n%s", s)
	}
}

func TestRunHelpUserDefinedWins(t *testing.T) {
	src := helpTestSource(t)
	var out bytes.Buffer
	// demo имеет команду help → user-defined help выигрывает (выполняется).
	if err := runHelp(&out, src, nil, []string{"demo"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "CUSTOM") {
		t.Fatalf("expected user-defined help to run, got:\n%s", out.String())
	}
}

func TestRunHelpUserDefinedSubcommandWins(t *testing.T) {
	src := helpTestSource(t)
	var out bytes.Buffer
	// demo.greet не имеет subcommand help → системная справка команды.
	if err := runHelp(&out, src, nil, []string{"demo", "greet"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out.String(), "CUSTOM") {
		t.Fatalf("unexpected user-defined help for greet: %s", out.String())
	}
}

func TestRunHelpRoot(t *testing.T) {
	// runHelp с пустым positional печатает usage в stderr (printUsage).
	src := helpTestSource(t)
	var out bytes.Buffer
	if err := runHelp(&out, src, nil, []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = out // printUsage пишет в os.Stderr напрямую; проверяем отсутствие ошибки
}

func TestRunHelpNotFound(t *testing.T) {
	src := helpTestSource(t)
	var out bytes.Buffer
	err := runHelp(&out, src, nil, []string{"demo", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected command name in error, got: %v", err)
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
	usage := captureStderr(t, printUsage)
	for _, w := range completion.ReservedNames {
		if !strings.Contains(usage, `"`+w+`"`) {
			t.Errorf("usage does not mention reserved word %q", w)
		}
	}
	if strings.Contains(usage, "hybrid mode") {
		t.Errorf("usage still mentions hybrid mode")
	}
}
