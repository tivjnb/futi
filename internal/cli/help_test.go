package cli

import (
	"bytes"
	"context"
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
//	demo.yaml    — команды greet (leaf с flags), db (ветка up/down), help (user-defined)
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
    flags:
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

// helpArgs — helpRequest для пути (futi help <path...>).
func helpArgs(path ...string) *helpRequest {
	return &helpRequest{path: path}
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
	code = runHelp(&out, &errOut, rt, req.path)
	return code, out.String(), errOut.String()
}

// runWordsFor runs the command words through runParsed with separate
// stdout/stderr buffers.
func runWordsFor(t *testing.T, rt *core.Runtime, words ...string) (code int, runErr error, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code, runErr = runParsed(context.Background(), nil, &out, &errOut, rt, &parsedArgs{words: words})
	return code, runErr, out.String(), errOut.String()
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
		t.Fatalf("expected command help with flags, got:\n%s", s)
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

// futi help <module> is system help even if the module has a command help.
func TestRunHelpSystemEvenWithUserHelpCommand(t *testing.T) {
	rt := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("demo"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "CUSTOM") || !strings.Contains(stdout, "Feature showcase") {
		t.Fatalf("expected system help of demo, got:\n%s", stdout)
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
//	m    — required-флаг x, печатает x и args в stdout, "ERRTEXT" в stderr
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
    args:
      - name: words
        variadic: true
    flags:
      - name: x
        type: string
        required: true
    cmd: ["sh", "-c", "echo x=$1 args=$2 $3; echo ERRTEXT >&2", "sh", "{{.x}}", "{{.words}}"]
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

// A trailing help is an ordinary user command: flags, args, streams and the
// exit code are those of the command.
func TestRunTrailingHelpUserCommandGetsFlagsAndArgs(t *testing.T) {
	rt := userHelpSource(t)
	code, _, stdout, stderr := runWordsFor(t, rt, "m", "help", "--x", "1", "--", "a", "b")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "x=1 args=a b") {
		t.Fatalf("stdout = %q, want it to contain %q", stdout, "x=1 args=a b")
	}
	if strings.Contains(stdout, "ERRTEXT") || !strings.Contains(stderr, "ERRTEXT") {
		t.Fatalf("stdout = %q, stderr = %q, want ERRTEXT only in stderr", stdout, stderr)
	}
}

func TestRunTrailingHelpUserCommandExitCode(t *testing.T) {
	rt := userHelpSource(t)
	code, runErr, _, stderr := runWordsFor(t, rt, "bad", "help")
	if code != 3 {
		t.Fatalf("code = %d, want 3 (stderr: %s)", code, stderr)
	}
	if runErr == nil {
		t.Fatal("ожидалась ошибка запуска для сигнального пути вызывающего")
	}
}

func TestRunTrailingHelpUserCommandMissingRequiredParam(t *testing.T) {
	rt := userHelpSource(t)
	code, _, _, stderr := runWordsFor(t, rt, "m", "help")
	if code != 1 || !strings.Contains(stderr, "required") {
		t.Fatalf("code = %d, stderr = %q, want 1 and a required error", code, stderr)
	}
}

func TestRunTrailingHelpUserCommandHelpFlagShowsItsHelp(t *testing.T) {
	rt := userHelpSource(t)
	code, _, stdout, stderr := runWordsFor(t, rt, "m", "help", "--help")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	// Исполнение дало бы ERRTEXT в stderr (или ошибку required): stderr пуст.
	if !strings.Contains(stdout, "Custom help text") || stderr != "" {
		t.Fatalf("stdout = %q, stderr = %q, want system help of the help command, not its execution", stdout, stderr)
	}
}

// futi help <path> describes a command named help instead of running it.
func TestRunHelpDescribesUserHelpCommand(t *testing.T) {
	rt := userHelpSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("m", "help"))
	if code != 0 || !strings.Contains(stdout, "Custom help text") || stderr != "" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q, want system help of m help", code, stdout, stderr)
	}
}

func TestRunHelpDottedPath(t *testing.T) {
	rt := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs("demo.greet"))
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "Greet someone\n\nusage: futi demo greet [flags]\n") || !strings.Contains(stdout, "--name") {
		t.Fatalf("expected help of command demo greet, got:\n%s", stdout)
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

// TestRunHelpPaths pins what futi help <path> prints for each kind of node.
func TestRunHelpPaths(t *testing.T) {
	dir := t.TempDir()
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
	writeFile(t, filepath.Join(dir, "bad.yaml"), "commands: [")
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
		{"module", helpArgs("branch"), 0, "Branch module\n\nusage: futi branch <subcommand>\nsubcommands:\n  help\n", ""},
		{"module without help command", helpArgs("plain"), 0, "Plain module\n\nusage: futi plain <subcommand>\nsubcommands:\n  run\n", ""},
		{"broken yaml", helpArgs("bad"), 1, "", "help: execmodule: parse manifest \"" + filepath.Join(dir, "bad.yaml") + "\": yaml: line 1: did not find expected node content\n"},
		{"unknown path", helpArgs("nope"), 1, "", "unknown command \"nope\" for \"futi\"\n"},
		{"unknown command", helpArgs("plain", "nope"), 1, "", "unknown command \"nope\" for \"futi plain\" (available: run)\n"},
		{"extra word after a leaf", helpArgs("plain", "run", "x"), 1, "", "unknown command \"x\" for \"futi plain run\"\n"},
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
