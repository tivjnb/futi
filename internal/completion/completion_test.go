package completion

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// newTestSource создаёт GlobalSource над временной директорией с демо-модулями:
//
//	docker.yaml   — команды ps, build (flags tag: string, force: bool), subcmds
//	git.yaml      — команда commit
//	dev/deploy.yaml — вложенный namespace
func newTestSource(t *testing.T) discovery.Source {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "docker.yaml"), `
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
      - name: force
        type: bool
        default: false
    cmd: "docker build -t {{.tag}} {{if .force}}-f{{end}}"
  migrate:
    description: Migrations
    commands:
      up:
        cmd: "migrate up"
      down:
        cmd: "migrate down"
`)

	writeFile(t, filepath.Join(dir, "meta.yaml"), `
name: meta
description: Image stats
cmd: "meta.py --files {{.files}}{{if .json}} --json{{end}}"
args:
  - name: files
    required: true
    variadic: true
flags:
  - name: json
    type: bool
    default: "false"
`)

	writeFile(t, filepath.Join(dir, "git.yaml"), `
name: git
description: Git shortcuts
commands:
  commit:
    description: Commit changes
    cmd: "git commit"
`)

	writeFile(t, filepath.Join(dir, "dev", "deploy.yaml"), `
name: dev.deploy
description: Deploy utilities
commands:
  staging:
    cmd: "deploy staging"
`)

	// Namespace-директория без config.yaml — чистый namespace.
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}

	return discovery.NewGlobalSource(dir)
}

func text(matches []Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Text
	}
	return out
}

func TestCompleteEmptyArgsRoot(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{}, "")
	got := text(matches)

	// builtins + корневые модули + namespace (отсортировано по алфавиту).
	want := []string{"cache", "completion", "dev", "docker", "git", "help", "list", "meta", "modules", "tools"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v, want NoFileComp", directive)
	}
}

func TestCompleteFilterByPrefix(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{}, "do")
	got := text(matches)
	want := []string{"docker"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteModuleCommands(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker"}, "")
	got := text(matches)
	want := []string{"build", "migrate", "ps"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteModuleCommandsFiltered(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker"}, "b")
	got := text(matches)
	want := []string{"build"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteSubcommands(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "migrate"}, "")
	got := text(matches)
	want := []string{"down", "up"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteLeafCommandNoSuggestions(t *testing.T) {
	// docker ps is a leaf without flags: nothing follows the path.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "ps"}, "")
	if len(matches) != 0 {
		t.Fatalf("unexpected matches: %v, want none", text(matches))
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteLeafCommandRequiredParams(t *testing.T) {
	// docker build: flag tag (required), force (bool, not required).
	// Flags are suggested only for a word starting with "-".
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build"}, "-")
	got := text(matches)
	want := []string{"--force", "--help", "--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteLeafRequiredParamHasDescription(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build"}, "-")
	found := false
	for _, m := range matches {
		if m.Text == "--tag" {
			found = true
			if m.Description != "(required)" {
				t.Fatalf("expected (required) description, got %q", m.Description)
			}
		}
	}
	if !found {
		t.Fatalf("expected --tag in matches, got %v", matches)
	}
}

func TestCompleteParamNames(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build"}, "--")
	got := text(matches)
	want := []string{"--force", "--help", "--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

// audit F83: слово пути через точку делится как пробелы.
func TestCompleteDottedPathParamNames(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker.build"}, "--")
	got := text(matches)
	want := []string{"--force", "--help", "--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteParamNamesFiltered(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build"}, "--t")
	got := text(matches)
	want := []string{"--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteParamEqualsNoSuggest(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build"}, "--tag=foo")
	if len(matches) != 0 {
		t.Fatalf("expected no matches for --param=val, got %v", matches)
	}
	// The value after "=" is free text: nothing to suggest.
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteAfterBoolFlagNoTrueFalse(t *testing.T) {
	// The parser rejects "--force true": after a bool flag the remaining
	// flags are suggested for a "-" word, without true/false.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build", "--force"}, "-")
	got := text(matches)
	want := []string{"--help", "--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteAfterFlagWithEqualsNoValueHint(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build", "--force=false"}, "-")
	got := text(matches)
	want := []string{"--help", "--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteNoPathWordsAfterFlag(t *testing.T) {
	// futi docker --tag x <TAB>: путь после флага невозможен, команды docker
	// (build/ps/migrate) предлагать нельзя.
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "--tag", "x"}, "")
	if len(matches) != 0 {
		t.Fatalf("expected no matches, got %v", text(matches))
	}
}

func TestCompleteStringValueNoSuggest(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build", "--tag"}, "")
	if len(matches) != 0 {
		t.Fatalf("expected no matches for string value, got %v", matches)
	}
	// The value of a string flag is free text: nothing to suggest.
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteDoubleDashNoSuggestions(t *testing.T) {
	// After "--" the words are data of the command, not flags or subcommands.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "ps", "--"}, "-")
	if len(matches) != 0 {
		t.Fatalf("expected no matches after --, got %v", matches)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v, want NoFileComp", directive)
	}
}

func TestCompleteNestedNamespace(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"dev"}, "")
	got := text(matches)
	want := []string{"deploy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteNamespaceModuleCommands(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"dev", "deploy"}, "")
	got := text(matches)
	want := []string{"staging"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteCacheSubcommands(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"cache"}, "")
	got := text(matches)
	want := []string{"clear", "refresh", "status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteCompletionShells(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"completion"}, "")
	got := text(matches)
	want := []string{"bash", "fish", "zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteModulesInitFlags(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"modules", "init"}, "--")
	got := text(matches)
	want := []string{"--command", "--default-cmd", "--desc", "--global"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteModulesSubcommands(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"modules"}, "")
	got := text(matches)
	want := []string{"add_command", "info", "init", "install", "list", "path", "remove_command", "uninstall", "update", "validate"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v, want NoFileComp", directive)
	}
}

func TestCompleteModulesInstallFlags(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"modules", "install"}, "--")
	got := text(matches)
	want := []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--ref", "--skip-attach", "--yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteModulesUpdateFlags(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"modules", "update"}, "--")
	got := text(matches)
	want := []string{"--skip-attach", "--yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteNonexistentNamespace(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"nope"}, "")
	if len(matches) != 0 {
		t.Fatalf("expected no matches, got %v", matches)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteModuleDescriptionsPresent(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker"}, "")
	for _, m := range matches {
		if m.Text == "ps" && m.Description != "List containers" {
			t.Fatalf("expected description for ps, got %q", m.Description)
		}
	}
}

func TestCompleteRootChildrenDescriptions(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{}, "")
	for _, m := range matches {
		if m.Text == "docker" && m.Description != "Docker management" {
			t.Fatalf("expected description for docker, got %q", m.Description)
		}
	}
}

// TestCompleteConflictHasNoDescription: для x.yaml + x/config.yaml подсказка x
// одна и без описания модуля.
func TestCompleteConflictHasNoDescription(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.yaml"), "name: x\ndescription: Описание-из-yaml\ncmd: echo x\n")
	writeFile(t, filepath.Join(dir, "x", "config.yaml"), "name: x\ndescription: Описание-из-пакета\ncmd: echo x\n")

	matches, _ := Complete(discovery.NewGlobalSource(dir), nil, []string{}, "x")
	n := 0
	for _, m := range matches {
		if m.Text == "x" {
			n++
			if m.Description != "" {
				t.Errorf("Description = %q, want пусто", m.Description)
			}
		}
	}
	if n != 1 {
		t.Errorf("подсказок x = %d, want 1: %v", n, matches)
	}
}

// TestCompleteDedupModuleAndNamespace: модуль x.yaml рядом с namespace x/
// (без config.yaml) даёт одну подсказку x с описанием модуля.
func TestCompleteDedupModuleAndNamespace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.yaml"), "name: x\ndescription: Модуль x\ncmd: echo x\n")
	writeFile(t, filepath.Join(dir, "x", "y.yaml"), "name: y\ncmd: echo y\n")

	matches, _ := Complete(discovery.NewGlobalSource(dir), nil, []string{}, "x")
	want := []Match{{Text: "x", Description: "Модуль x"}}
	if !reflect.DeepEqual(matches, want) {
		t.Fatalf("matches = %+v, want %+v", matches, want)
	}
}

// newHelpSource создаёт модуль с user-defined командой "help", чтобы проверить,
// что системная подсказка help не дублируется.
func newHelpSource(t *testing.T) discovery.Source {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mod.yaml"), `
name: mod
description: Module with user help
commands:
  help:
    description: User-defined help
    cmd: "echo custom"
  run:
    description: Run something
    cmd: "echo run"
`)
	return discovery.NewGlobalSource(dir)
}

func TestCompleteNoTrailingHelpOrListWord(t *testing.T) {
	// help and list are no longer system words after a path: they are
	// suggested only when the node has such a command.
	src := newTestSource(t)
	for _, args := range [][]string{{"docker"}, {"docker", "migrate"}, {"docker", "ps"}, {"dev"}} {
		matches, _ := Complete(src, nil, args, "")
		if got := text(matches); contains(got, "help") || contains(got, "list") {
			t.Errorf("Complete(%v) = %v, want no help or list", args, got)
		}
	}
}

func TestCompleteUserHelpCommand(t *testing.T) {
	// A user-defined command "help" of a module is suggested as any command.
	src := newHelpSource(t)
	matches, _ := Complete(src, nil, []string{"mod"}, "")
	got := text(matches)
	want := []string{"help", "run"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestCompleteLeafDefaultParamHasDescription(t *testing.T) {
	// docker build: force (bool, default: false) — пометка (default: ...)
	// и на пустом toComplete, и в ветке "--".
	src := newTestSource(t)
	for _, toComplete := range []string{"-", "--"} {
		matches, _ := Complete(src, nil, []string{"docker", "build"}, toComplete)
		found := false
		for _, m := range matches {
			if m.Text == "--force" {
				found = true
				if m.Description != "(default: false)" {
					t.Fatalf("toComplete=%q: expected (default: false) description, got %q", toComplete, m.Description)
				}
			}
		}
		if !found {
			t.Fatalf("toComplete=%q: expected --force in matches, got %v", toComplete, matches)
		}
	}
}

func TestCompleteModuleDefaultCmdParams(t *testing.T) {
	// A single-command module (cmd + flags, no commands): the flags of its
	// own command follow the path.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"meta"}, "-")
	got := text(matches)
	want := []string{"--help", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}

	// После bool-флага --json — оставшиеся флаги, без true/false.
	matches, _ = Complete(src, nil, []string{"meta", "--json"}, "-")
	got = text(matches)
	if want := []string{"--help"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after bool flag matches: %v, want %v", got, want)
	}
}

func TestCompleteAfterFlagValue(t *testing.T) {
	// The value of a flag does not become a path word: the remaining flags
	// are suggested for a "-" word.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build", "--tag", "v1.0"}, "-")
	got := text(matches)
	want := []string{"--force", "--help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("- toComplete: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}

	matches, _ = Complete(src, nil, []string{"docker", "build", "--tag", "v1.0"}, "--")
	got = text(matches)
	want = []string{"--force", "--help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("-- toComplete: %v, want %v", got, want)
	}
}

func TestCompleteFlagValueNoSuggest(t *testing.T) {
	// The value of a non-bool flag is free text (files only for future
	// path/dir types): nothing is suggested, the shell does not add files.
	src := newTestSource(t)
	for _, toComplete := range []string{"", "~/qr", "--"} {
		matches, directive := Complete(src, nil, []string{"docker", "build", "--tag"}, toComplete)
		if len(matches) != 0 || directive != ShellCompDirectiveNoFileComp {
			t.Errorf("toComplete=%q: %v/%v, want none/NoFileComp", toComplete, text(matches), directive)
		}
	}
}

func TestCompleteStringFlagValuePathStaysResolved(t *testing.T) {
	// futi docker build --tag myapp -<TAB>: myapp is the flag value, the path
	// stays docker build; the remaining flags are suggested.
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build", "--tag", "myapp"}, "-")
	got := text(matches)
	want := []string{"--force", "--help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteBuiltinFlagsKeepBehavior(t *testing.T) {
	// Builtins с флагами (модуль не зарезолвен).
	src := newTestSource(t)
	cases := []struct {
		args []string
		want Directive
	}{
		// флаг подкоманды, неизвестный completion — значение user-supplied: файлы
		{[]string{"modules", "update", "--all"}, ShellCompDirectiveDefault},
		// после завершённого значения/короткого флага — подсказок нет
		{[]string{"modules", "update", "--skip-attach", "foo"}, ShellCompDirectiveNoFileComp},
		{[]string{"cache", "refresh", "-v"}, ShellCompDirectiveNoFileComp},
	}
	for _, c := range cases {
		matches, directive := Complete(src, nil, c.args, "")
		if len(matches) != 0 || directive != c.want {
			t.Errorf("args=%v: matches=%v directive=%v, want none/%v", c.args, text(matches), directive, c.want)
		}
	}
}

// TestCompleteModulesAfterFlag: после флага `futi modules <sub>` без "=":
// флаг со значением и источник install — файлы; булев флаг — оставшиеся флаги.
func TestCompleteModulesAfterFlag(t *testing.T) {
	src := newTestSource(t)
	cases := []struct {
		args []string
		want []string
		dir  Directive
	}{
		{[]string{"modules", "install", "x", "--local"},
			[]string{"--from-archive", "--from-git", "--from-local", "--namespace", "--ref", "--skip-attach", "--yes"}, ShellCompDirectiveNoFileComp},
		{[]string{"modules", "install", "--local"}, nil, ShellCompDirectiveDefault},
		{[]string{"modules", "install", "--namespace"}, nil, ShellCompDirectiveDefault},
		{[]string{"modules", "install", "--namespace", "ns", "--yes"}, nil, ShellCompDirectiveDefault},
		{[]string{"modules", "install", "x", "--namespace", "ns", "--yes"},
			[]string{"--from-archive", "--from-git", "--from-local", "--local", "--ref", "--skip-attach"}, ShellCompDirectiveNoFileComp},
		{[]string{"modules", "install", "--namespace", "ns", "src", "--yes"},
			[]string{"--from-archive", "--from-git", "--from-local", "--local", "--ref", "--skip-attach"}, ShellCompDirectiveNoFileComp},
		{[]string{"modules", "uninstall", "foo", "--force"},
			[]string{"--skip-detach", "--strict-detach", "--yes"}, ShellCompDirectiveNoFileComp},
		{[]string{"modules", "init", "--global"},
			[]string{"--command", "--default-cmd", "--desc"}, ShellCompDirectiveNoFileComp},
	}
	for _, c := range cases {
		matches, directive := Complete(src, nil, c.args, "")
		if got := text(matches); !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) || directive != c.dir {
			t.Errorf("args=%v: matches=%v directive=%v, want %v/%v", c.args, got, directive, c.want, c.dir)
		}
	}
}

func TestCompleteAfterShortHelpFlag(t *testing.T) {
	// -h is a flag token: the path does not grow after it, the flags of the
	// command are still suggested (-h is not a used flag).
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build", "-h"}, "-")
	got := text(matches)
	want := []string{"--force", "--help", "--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteExcludesUsedFlags(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build", "--tag=x", "--force"}, "-")
	if got := text(matches); !reflect.DeepEqual(got, []string{"--help"}) {
		t.Fatalf("- toComplete: %v, want [--help]", got)
	}
	matches, _ = Complete(src, nil, []string{"docker", "build", "--tag=x"}, "--")
	if got := text(matches); !reflect.DeepEqual(got, []string{"--force", "--help"}) {
		t.Fatalf("-- toComplete: %v, want [--force --help]", got)
	}
}

func TestCompleteModulesFlagsAfterPositional(t *testing.T) {
	// Флаги `futi modules <sub>` должны предлагаться даже если после
	// подкоманды есть позиционный аргумент (URL, namespace).
	// Сценарий: futi __complete modules install https://x/repo.git --
	src := newTestSource(t)

	// Случай 1: modules install с URL
	matches, _ := Complete(src, nil, []string{"modules", "install", "https://x/repo.git"}, "--")
	got := text(matches)
	want := []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--ref", "--skip-attach", "--yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install with URL: got %v, want %v", got, want)
	}

	// Случай 2: modules uninstall с namespace
	matches, _ = Complete(src, nil, []string{"modules", "uninstall", "dev.tool"}, "--")
	got = text(matches)
	want = []string{"--force", "--skip-detach", "--strict-detach", "--yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uninstall with namespace: got %v, want %v", got, want)
	}

	// Случай 3: modules install с --ref перед URL (флаг уже введён)
	matches, _ = Complete(src, nil, []string{"modules", "install", "--ref", "v1", "https://x/repo.git"}, "--")
	got = text(matches)
	want = []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--skip-attach", "--yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install with --ref before URL: got %v, want %v", got, want)
	}

	// Случай 4: modules install с URL и --ref после (флаг уже введён)
	matches, _ = Complete(src, nil, []string{"modules", "install", "https://x/repo.git", "--ref", "v1"}, "--")
	got = text(matches)
	want = []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--skip-attach", "--yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install with --ref after URL: got %v, want %v", got, want)
	}

	// Случай 5: modules info (нет флагов)
	matches, _ = Complete(src, nil, []string{"modules", "info", "x"}, "--")
	got = text(matches)
	want = []string{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("info with namespace: got %v, want %v", got, want)
	}
}

func TestCompletePartialListingSuggestsReadableSources(t *testing.T) {
	dirA := t.TempDir()
	writeFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ncmd: echo x\n")
	missing := filepath.Join(t.TempDir(), "missing")
	src := discovery.NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: missing}}})

	matches, _ := Complete(src, nil, nil, "")
	found := false
	for _, s := range text(matches) {
		if s == "x" {
			found = true
		}
	}
	if !found {
		t.Errorf("подсказки %v не содержат x", text(matches))
	}
}

func TestIsReservedName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"list", true},
		{"cache", true},
		{"modules", true},
		{"completion", true},
		{"__complete", true},
		{"help", true},
		{"validate", false},
		{"init", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsReservedName(tt.name); got != tt.want {
			t.Errorf("IsReservedName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
	if len(ReservedNames) != 6 {
		t.Errorf("ReservedNames = %v, want 6 names", ReservedNames)
	}
}

// newReservedSource — источник с модулями, чьи имена совпадают с builtins корня.
func newReservedSource(t *testing.T, names ...string) discovery.Source {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		writeFile(t, filepath.Join(dir, n+".yaml"), "name: "+n+"\ndescription: Module "+n+"\ncommands:\n  modcmd:\n    cmd: \"echo\"\n")
	}
	return discovery.NewGlobalSource(dir)
}

func TestCompleteBuiltinWinsOverModule(t *testing.T) {
	src := newReservedSource(t, "cache", "modules", "completion")
	tests := []struct {
		arg  string
		want []string
	}{
		{"cache", []string{"clear", "refresh", "status"}},
		{"modules", []string{"add_command", "info", "init", "install", "list", "path", "remove_command", "uninstall", "update", "validate"}},
		{"completion", []string{"bash", "fish", "zsh"}},
	}
	for _, tc := range tests {
		t.Run(tc.arg, func(t *testing.T) {
			matches, directive := Complete(src, nil, []string{tc.arg}, "")
			if got := text(matches); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Complete([%q]) = %v, want %v", tc.arg, got, tc.want)
			}
			if directive != ShellCompDirectiveNoFileComp {
				t.Fatalf("directive = %v, want NoFileComp", directive)
			}
		})
	}
}

func TestCompleteRootBuiltinNotDuplicatedByModule(t *testing.T) {
	src := newReservedSource(t, "cache", "dev")
	matches, _ := Complete(src, nil, []string{}, "")
	want := []string{"cache", "completion", "dev", "help", "list", "modules"}
	if got := text(matches); !reflect.DeepEqual(got, want) {
		t.Fatalf("root matches = %v, want %v", got, want)
	}
	for _, m := range matches {
		if m.Text == "cache" && m.Description != "" {
			t.Fatalf("builtin cache has module description %q", m.Description)
		}
	}
}

func TestCompleteListFirstWordIgnoresModule(t *testing.T) {
	src := newReservedSource(t, "list")
	// The first word is the builtin: the commands of the module (modcmd) are
	// not offered; the module itself is a path word like any other.
	matches, directive := Complete(src, nil, []string{"list"}, "")
	if got := text(matches); !reflect.DeepEqual(got, []string{"list"}) {
		t.Fatalf("Complete([list]) = %v, want [list]", got)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
}

func TestCompleteModulesValidate(t *testing.T) {
	src := newTestSource(t)
	tests := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
	}{
		{"all", []string{"modules", "validate"}, "", []string{"dev.deploy", "docker", "git", "meta"}},
		{"prefix", []string{"modules", "validate"}, "de", []string{"dev.deploy"}},
		{"after first argument", []string{"modules", "validate", "dev.deploy"}, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matches, directive := Complete(src, nil, tc.args, tc.toComplete)
			if got := text(matches); len(got) != len(tc.want) || (len(got) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("Complete(%v, %q) = %v, want %v", tc.args, tc.toComplete, got, tc.want)
			}
			if directive != ShellCompDirectiveNoFileComp {
				t.Fatalf("directive = %v, want NoFileComp", directive)
			}
		})
	}
}

// partialSource — источник, чей List возвращает валидный результат вместе
// с *discovery.PartialError (часть источников не читается).
type partialSource struct{ discovery.Source }

func (p partialSource) List() ([]discovery.ModuleInfo, error) {
	mods, err := p.Source.List()
	if err != nil {
		return nil, err
	}
	return mods, &discovery.PartialError{Errs: []error{errors.New("unreadable source")}}
}

func TestCompleteModulesValidatePartialListing(t *testing.T) {
	src := partialSource{newTestSource(t)}
	matches, directive := Complete(src, nil, []string{"modules", "validate"}, "d")
	want := []string{"dev.deploy", "docker"}
	if got := text(matches); !reflect.DeepEqual(got, want) {
		t.Fatalf("Complete with PartialError = %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
}

// TestCompleteListKeyword фиксирует уровни, где `futi <path> list` выполняется
// успешно (проверено сборкой бинаря): namespace, модуль, ветка с подкомандами,
// модуль с default cmd. На листовой команде list даёт ошибку — не подсказываем.
func TestCompleteModulesAddCommandFlags(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"modules", "add_command", "docker"}, "--")
	got := text(matches)
	want := []string{"--cmd", "--desc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteModulesRemoveCommandNoFlags(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"modules", "remove_command", "docker"}, "--")
	if len(matches) != 0 {
		t.Fatalf("remove_command has no flags, got %v", matches)
	}
}

// partialChildrenSource — ListChildren возвращает валидных детей вместе с
// *discovery.PartialError (часть источников не читается).
type partialChildrenSource struct{ discovery.Source }

func (p partialChildrenSource) ListChildren(ns string) ([]discovery.Child, error) {
	children, err := p.Source.ListChildren(ns)
	if err != nil {
		return nil, err
	}
	return children, &discovery.PartialError{Errs: []error{errors.New("unreadable source")}}
}

func TestCompletePartialChildrenListing(t *testing.T) {
	src := partialChildrenSource{newTestSource(t)}
	matches, _ := Complete(src, nil, []string{"dev"}, "")
	want := []string{"deploy"}
	if got := text(matches); !reflect.DeepEqual(got, want) {
		t.Fatalf("Complete(dev) with PartialError = %v, want %v", got, want)
	}
}

// TestModulesFlagsReturnsCopy: изменение возвращённого среза не влияет на
// следующий вызов.
func TestModulesFlagsReturnsCopy(t *testing.T) {
	first := ModulesFlags("init")
	if len(first) == 0 {
		t.Fatal("ModulesFlags(init) пуст")
	}
	want := first[0]
	first[0] = "--mutated"
	if got := ModulesFlags("init")[0]; got != want {
		t.Errorf("ModulesFlags(init)[0] = %q, want %q", got, want)
	}
}

func TestCompleteParamsReloadsChangedManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.yaml")
	writeFile(t, path, `
name: stale
commands:
  run:
    flags:
      - name: old
        type: string
    cmd: "echo {{.old}}"
`)
	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	manifest, root, err := src.Resolve("stale")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := cache.LoadModule(manifest, root); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	writeFile(t, path, `
name: stale
commands:
  run:
    flags:
      - name: fresh
        type: string
    cmd: "echo {{.fresh}}"
`)
	matches, _ := Complete(src, cache, []string{"stale", "run"}, "--")
	if got := text(matches); !contains(got, "--fresh") || contains(got, "--old") {
		t.Errorf("Complete = %v, want --fresh and no --old", got)
	}
}

// Путь moduleMatches (подсказка команд модуля без "--") при устаревшей записи.
func TestCompleteCommandsReloadsChangedManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.yaml")
	writeFile(t, path, `
name: stale
commands:
  oldcmd:
    cmd: "echo old"
`)
	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	manifest, root, err := src.Resolve("stale")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := cache.LoadModule(manifest, root); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	writeFile(t, path, `
name: stale
commands:
  newcmd:
    cmd: "echo new"
`)
	matches, _ := Complete(src, cache, []string{"stale"}, "")
	if got := text(matches); !contains(got, "newcmd") || contains(got, "oldcmd") {
		t.Errorf("Complete = %v, want newcmd and no oldcmd", got)
	}
}

func TestCompleteDryRunPrefix(t *testing.T) {
	src := newTestSource(t)
	got, gotDir := Complete(src, nil, []string{"--dry-run"}, "")
	var gotTexts []string
	for _, m := range got {
		gotTexts = append(gotTexts, m.Text)
	}
	// Корень без builtins (cache, completion, list, modules): модули на месте.
	wantTexts := []string{"dev", "docker", "git", "meta", "tools"}
	if !reflect.DeepEqual(gotTexts, wantTexts) || gotDir != ShellCompDirectiveNoFileComp {
		t.Fatalf("Complete после --dry-run = %v/%v, want %v", gotTexts, gotDir, wantTexts)
	}
	if m, _ := Complete(src, nil, []string{"--dry-run", "cache"}, ""); len(m) != 0 {
		t.Fatalf("подкоманды builtin с --dry-run не подсказываются, got %v", m)
	}

	matches, _ := Complete(src, nil, nil, "--d")
	var texts []string
	for _, m := range matches {
		texts = append(texts, m.Text)
	}
	if !reflect.DeepEqual(texts, []string{"--dry-run"}) {
		t.Fatalf("кандидаты на --d = %v, want [--dry-run]", texts)
	}
	matches, _ = Complete(src, nil, nil, "--")
	texts = nil
	for _, m := range matches {
		texts = append(texts, m.Text)
	}
	if !reflect.DeepEqual(texts, []string{"--dry-run", "--help", "--version", "--yes"}) {
		t.Fatalf("кандидаты на -- = %v", texts)
	}
}

func TestCompleteAfterDryRunNoGlobalFlags(t *testing.T) {
	src := newTestSource(t)
	// Only the other mutating-run flag may follow; --help/--version may not.
	if got := completeTexts(t, src, []string{"--dry-run"}, "--"); !reflect.DeepEqual(got, []string{"--yes"}) {
		t.Fatalf("после --dry-run подсказывается только --yes, got %v", got)
	}
}

// newGrammarSource builds modules that exercise the args/flags grammar.
func newGrammarSource(t *testing.T) discovery.Source {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "k8s.yaml"), `
name: k8s
description: Kubernetes
commands:
  logs:
    description: Show logs
    cmd: "kubectl logs {{.pod}} {{if .follow}}-f{{end}} {{if .tail}}--tail={{.tail}}{{end}}"
    args:
      - {name: pod, required: true}
    flags:
      - {name: follow, type: bool, short: f}
      - {name: tail}
`)
	writeFile(t, filepath.Join(dir, "docker.yaml"), `
name: docker
description: Docker
commands:
  build:
    description: Build image
    cmd: "docker build -t {{.tag}} -f {{.file}} {{.context}}"
    args:
      - {name: context, default: "."}
    flags:
      - {name: tag, short: t, required: true}
      - {name: file, default: Dockerfile}
  run:
    description: Run container
    cmd: "docker run {{if .rm}}--rm{{end}} {{.image}} {{.docker_args}}"
    args:
      - {name: image, required: true}
      - {name: docker_args, rest: true}
    flags:
      - {name: rm, type: bool}
`)
	writeFile(t, filepath.Join(dir, "db.yaml"), `
name: db
description: Database
commands:
  migrate:
    description: Migrate
    cmd: "migrate"
    commands:
      up: {cmd: "migrate up"}
      down: {cmd: "migrate down"}
`)
	return discovery.NewGlobalSource(dir)
}

func TestCompleteGrammar(t *testing.T) {
	src := newGrammarSource(t)
	cases := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
		dir        Directive
	}{
		{"positional value", []string{"k8s", "logs"}, "", nil, ShellCompDirectiveNoFileComp},
		{"flags of node", []string{"k8s", "logs"}, "-", []string{"--follow", "--help", "--tail", "-f"}, ShellCompDirectiveNoFileComp},
		{"long flags only", []string{"k8s", "logs"}, "--", []string{"--follow", "--help", "--tail"}, ShellCompDirectiveNoFileComp},
		{"used flag removed", []string{"k8s", "logs", "--follow"}, "-", []string{"--help", "--tail"}, ShellCompDirectiveNoFileComp},
		{"flag value pending", []string{"docker", "build", "--tag"}, "", nil, ShellCompDirectiveNoFileComp},
		{"flag value with equals", []string{"docker", "build"}, "--tag=x", nil, ShellCompDirectiveNoFileComp},
		{"rest started", []string{"docker", "run", "nginx"}, "", nil, ShellCompDirectiveDefault},
		{"flag-like word in rest", []string{"docker", "run", "nginx"}, "--", nil, ShellCompDirectiveDefault},
		{"rest after double dash", []string{"docker", "run", "--", "nginx"}, "-", nil, ShellCompDirectiveDefault},
		{"flags before rest", []string{"docker", "run"}, "--", []string{"--help", "--rm"}, ShellCompDirectiveNoFileComp},
		{"data after double dash", []string{"k8s", "logs", "--"}, "", nil, ShellCompDirectiveNoFileComp},
		{"flag-like data after double dash", []string{"k8s", "logs", "--"}, "-", nil, ShellCompDirectiveNoFileComp},
		{"module subcommands", []string{"db"}, "", []string{"migrate"}, ShellCompDirectiveNoFileComp},
		{"nested subcommands", []string{"db", "migrate"}, "", []string{"down", "up"}, ShellCompDirectiveNoFileComp},
		{"subcommands of node with flags", []string{"db", "migrate"}, "-", []string{"--help"}, ShellCompDirectiveNoFileComp},
		{"subcommand only right after path", []string{"db", "migrate", "--x"}, "u", nil, ShellCompDirectiveNoFileComp},
		{"help path", []string{"help"}, "do", []string{"docker"}, ShellCompDirectiveNoFileComp},
		{"help nested path", []string{"help", "db"}, "", []string{"migrate"}, ShellCompDirectiveNoFileComp},
		{"list path", []string{"list", "docker"}, "b", []string{"build"}, ShellCompDirectiveNoFileComp},
		{"unknown path", []string{"nosuch"}, "", nil, ShellCompDirectiveNoFileComp},
		{"root builtins and modules", nil, "", []string{"cache", "completion", "db", "docker", "help", "k8s", "list", "modules"}, ShellCompDirectiveNoFileComp},
		{"module root has no help or list word", []string{"docker"}, "", []string{"build", "run"}, ShellCompDirectiveNoFileComp},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matches, dir := Complete(src, nil, c.args, c.toComplete)
			got := text(matches)
			if (!reflect.DeepEqual(got, c.want) && (len(got) != 0 || len(c.want) != 0)) || dir != c.dir {
				t.Errorf("Complete(%v, %q) = %v/%v, want %v/%v", c.args, c.toComplete, got, dir, c.want, c.dir)
			}
		})
	}
}

func TestCompleteCollisionGivesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev.yaml"), "name: dev\ncommands:\n  deploy: {cmd: \"x\"}\n")
	writeFile(t, filepath.Join(dir, "dev", "deploy.yaml"), "name: dev.deploy\ncmd: \"y\"\n")
	matches, dir2 := Complete(discovery.NewGlobalSource(dir), nil, []string{"dev", "deploy"}, "")
	if len(matches) != 0 || dir2 != ShellCompDirectiveNoFileComp {
		t.Errorf("Complete over a path collision = %v/%v, want none/NoFileComp", text(matches), dir2)
	}
}

func TestCompleteBrokenModuleGivesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.yaml"), "name: bad\ncommands: [oops\n")
	matches, dir2 := Complete(discovery.NewGlobalSource(dir), nil, []string{"bad"}, "")
	if len(matches) != 0 || dir2 != ShellCompDirectiveNoFileComp {
		t.Errorf("Complete over a broken module = %v/%v, want none/NoFileComp", text(matches), dir2)
	}
}

// The reserved-name filter belongs to the first word only (builtins win
// there); `futi help <TAB>` and `futi list <TAB>` offer a module of any name.
func TestCompleteHelpAndListOfferReservedModuleNames(t *testing.T) {
	src := newReservedSource(t, "cache", "list", "dev")
	for _, kw := range []string{"help", "list"} {
		matches, dir := Complete(src, nil, []string{kw}, "")
		want := []string{"cache", "dev", "list"}
		if got := text(matches); !reflect.DeepEqual(got, want) || dir != ShellCompDirectiveNoFileComp {
			t.Errorf("Complete([%s]) = %v/%v, want %v/NoFileComp", kw, got, dir, want)
		}
	}
	// The first word is still not a module of a reserved name.
	matches, _ := Complete(src, nil, nil, "")
	want := []string{"cache", "completion", "dev", "help", "list", "modules"}
	if got := text(matches); !reflect.DeepEqual(got, want) {
		t.Errorf("root = %v, want %v", got, want)
	}
}

// A node whose first positional is the rest arg and that has no subcommands
// hands the first word to the shell (files); with subcommands it suggests them.
func TestCompleteRestOnlyNodeUsesShellFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "run.yaml"), "name: run\ncmd: \"go run {{.go_args}}\"\nargs:\n  - {name: go_args, rest: true}\n")
	writeFile(t, filepath.Join(dir, "sh.yaml"), "name: sh\ncmd: \"x {{.a}}\"\nargs:\n  - {name: a, rest: true}\ncommands:\n  up: {cmd: \"x up\"}\n")
	writeFile(t, filepath.Join(dir, "img.yaml"), "name: img\ncmd: \"x {{.image}} {{.a}}\"\nargs:\n  - {name: image, required: true}\n  - {name: a, rest: true}\n")
	src := discovery.NewGlobalSource(dir)
	cases := []struct {
		name string
		args []string
		want []string
		dir  Directive
	}{
		{"rest-only module", []string{"run"}, nil, ShellCompDirectiveDefault},
		{"with subcommands", []string{"sh"}, []string{"up"}, ShellCompDirectiveNoFileComp},
		{"image comes first", []string{"img"}, nil, ShellCompDirectiveNoFileComp},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matches, d := Complete(src, nil, c.args, "")
			if got := text(matches); (!reflect.DeepEqual(got, c.want) && (len(got) != 0 || len(c.want) != 0)) || d != c.dir {
				t.Errorf("Complete(%v) = %v/%v, want %v/%v", c.args, got, d, c.want, c.dir)
			}
		})
	}
}

func TestCompleteInputTypes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cp.yaml"), `name: cp
cmd: "cp {{.src}} {{.dst}}"
args:
  - {name: src, type: file, variadic: true}
  - {name: dst, type: dir, required: true}
`)
	writeFile(t, filepath.Join(dir, "mk.yaml"), "name: mk\ncmd: \"mkdir {{.d}}\"\nargs:\n  - {name: d, type: dir}\n")
	writeFile(t, filepath.Join(dir, "log.yaml"), `name: log
cmd: "log {{.level}} {{.out}}"
flags:
  - {name: level, short: l, choices: [debug, info, warn]}
  - {name: out, type: file}
`)
	writeFile(t, filepath.Join(dir, "dep.yaml"), `name: dep
cmd: "dep {{.target}}"
args:
  - {name: target, choices: [dev, prod, status-x]}
commands:
  status: {cmd: "dep status"}
`)
	writeFile(t, filepath.Join(dir, "run.yaml"), "name: run\ncmd: \"run {{.x}}\"\nargs:\n  - {name: x}\n")
	writeFile(t, filepath.Join(dir, "two.yaml"), `name: two
cmd: "two {{.a}} {{.b}}"
args:
  - {name: a, choices: [p, q]}
  - {name: b, choices: [r, s]}
`)
	writeFile(t, filepath.Join(dir, "merge.yaml"), `name: merge
cmd: "merge {{.a}} {{.b}}"
args:
  - {name: a, variadic: true, required: true, choices: [x, z]}
  - {name: b, required: true, choices: [y, z]}
`)
	writeFile(t, filepath.Join(dir, "dflag.yaml"), "name: dflag\ncmd: \"d {{.d}}\"\nflags:\n  - {name: d, type: dir}\n")
	src := discovery.NewGlobalSource(dir)
	cases := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
		dir        Directive
	}{
		{"choices of several candidates are merged and deduplicated", []string{"merge"}, "", []string{"x", "y", "z"}, ShellCompDirectiveNoFileComp},
		{"dir flag value", []string{"dflag", "--d"}, "", nil, ShellCompDirectiveFilterDirs},
		{"dir flag=value", []string{"dflag"}, "--d=x", nil, ShellCompDirectiveNoFileComp},
		{"choices of a non-first positional", []string{"two", "x"}, "", []string{"r", "s"}, ShellCompDirectiveNoFileComp},
		{"choices of a positional after double dash", []string{"two", "--"}, "", []string{"p", "q"}, ShellCompDirectiveNoFileComp},
		{"file wins over dir", []string{"cp"}, "", nil, ShellCompDirectiveDefault},
		{"variadic file after first word", []string{"cp", "a"}, "", nil, ShellCompDirectiveDefault},
		{"dir only", []string{"mk"}, "", nil, ShellCompDirectiveFilterDirs},
		{"long flag choices", []string{"log", "--level"}, "", []string{"debug", "info", "warn"}, ShellCompDirectiveNoFileComp},
		{"short flag choices", []string{"log", "-l"}, "i", []string{"info"}, ShellCompDirectiveNoFileComp},
		{"flag=value choices", []string{"log"}, "--level=de", []string{"--level=debug"}, ShellCompDirectiveNoFileComp},
		{"flag=value file", []string{"log"}, "--out=x", nil, ShellCompDirectiveNoFileComp},
		{"file flag value", []string{"log", "--out"}, "", nil, ShellCompDirectiveDefault},
		{"subcommand prefix wins", []string{"dep"}, "st", []string{"status"}, ShellCompDirectiveNoFileComp},
		{"unmatched prefix gives choices", []string{"dep"}, "p", []string{"prod"}, ShellCompDirectiveNoFileComp},
		{"plain string", []string{"run"}, "", nil, ShellCompDirectiveNoFileComp},
		{"after double dash", []string{"cp", "--"}, "", nil, ShellCompDirectiveDefault},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matches, d := Complete(src, nil, c.args, c.toComplete)
			if got := text(matches); (!reflect.DeepEqual(got, c.want) && (len(got) != 0 || len(c.want) != 0)) || d != c.dir {
				t.Errorf("Complete(%v, %q) = %v/%v, want %v/%v", c.args, c.toComplete, got, d, c.want, c.dir)
			}
		})
	}
}

func completeTexts(t *testing.T, src discovery.Source, args []string, toComplete string) []string {
	t.Helper()
	matches, _ := Complete(src, nil, args, toComplete)
	var texts []string
	for _, m := range matches {
		texts = append(texts, m.Text)
	}
	return texts
}

func TestCompleteGlobalFlagsYes(t *testing.T) {
	src := newTestSource(t)
	cases := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
	}{
		{"all global flags", nil, "--", []string{"--dry-run", "--help", "--version", "--yes"}},
		{"after --yes", []string{"--yes"}, "--", []string{"--dry-run"}},
		{"after -y", []string{"-y"}, "--", []string{"--dry-run"}},
		{"after both", []string{"--dry-run", "--yes"}, "-", nil},
		{"after both reversed", []string{"--yes", "--dry-run"}, "-", nil},
		{"after --dry-run", []string{"--dry-run"}, "--", []string{"--yes"}},
		{"paths after -y", []string{"-y"}, "", []string{"dev", "docker", "git", "meta", "tools"}},
		{"prefix after --yes", []string{"--yes"}, "do", []string{"docker"}},
		{"builtin subcommand after --yes", []string{"--yes", "cache"}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := completeTexts(t, src, tc.args, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Complete(%q, %q) = %v, want %v", tc.args, tc.toComplete, got, tc.want)
			}
		})
	}
}

// exposeSource: module ops (root cli+mcp; status inherits, prod and the group
// grp are mcp only), ns/hidden (mcp only) and ns/shown.
func exposeSource(t *testing.T) discovery.Source {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ops.yaml"), `expose: [cli, mcp]
commands:
  status:
    cmd: echo status
  prod:
    expose: [mcp]
    flags:
      - name: force
        type: bool
    cmd: "echo prod {{if .force}}-f{{end}}"
  grp:
    expose: [mcp]
    commands:
      a:
        cmd: echo a
`)
	writeFile(t, filepath.Join(dir, "ns", "hidden.yaml"), "expose: [mcp]\ncmd: echo hidden\n")
	writeFile(t, filepath.Join(dir, "ns", "shown.yaml"), "cmd: echo shown\n")
	return discovery.NewGlobalSource(dir)
}

func TestCompleteHidesNotExposed(t *testing.T) {
	src := exposeSource(t)
	if got := completeTexts(t, src, []string{"ops"}, ""); !reflect.DeepEqual(got, []string{"status"}) {
		t.Errorf("ops children = %v, want [status]", got)
	}
	if got := completeTexts(t, src, []string{"ns"}, ""); !reflect.DeepEqual(got, []string{"shown"}) {
		t.Errorf("ns children = %v, want [shown]", got)
	}
	if got := completeTexts(t, src, []string{"ops", "prod"}, "-"); got != nil {
		t.Errorf("flags of hidden command = %v, want none", got)
	}
	if got := completeTexts(t, src, []string{"ops", "grp"}, ""); got != nil {
		t.Errorf("children of hidden group = %v, want none", got)
	}
	if got := completeTexts(t, src, []string{"ops", "status"}, "-"); !reflect.DeepEqual(got, []string{"--help"}) {
		t.Errorf("flags of visible command = %v, want [--help]", got)
	}
}

func TestCompleteHiddenModuleStillOffersNamespaceChildren(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ops", "secret.yaml"), `expose: [mcp]
cmd: "echo secret {{if .force}}-f{{end}}"
flags:
  - name: force
    type: bool
commands:
  inner:
    cmd: echo inner
`)
	writeFile(t, filepath.Join(dir, "ops", "secret", "kid.yaml"), "cmd: echo kid\n")
	src := discovery.NewGlobalSource(dir)
	if got := completeTexts(t, src, []string{"ops", "secret"}, ""); !reflect.DeepEqual(got, []string{"kid"}) {
		t.Errorf("children of hidden module = %v, want [kid] (no subcommands)", got)
	}
	if got := completeTexts(t, src, []string{"ops", "secret"}, "-"); got != nil {
		t.Errorf("flags of hidden module = %v, want none", got)
	}
}
