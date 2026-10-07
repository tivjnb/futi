package completion

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/discovery"
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
//	docker.yaml   — команды ps, build (params tag: string, force: bool), subcmds
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
    params:
      - name: tag
        type: string
        required: true
      - name: force
        type: bool
        default: false
    cmd: "docker build -t {{.tag}}"
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
cmd: "meta.py --files {{quote .args}}{{if .json}} --json{{end}}"
params:
  - name: json
    type: bool
    default: "false"
positional:
  min: 1
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
	want := []string{"cache", "completion", "dev", "docker", "git", "list", "meta", "modules", "tools"}
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
	want := []string{"build", "help", "migrate", "ps"}
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
	want := []string{"down", "help", "up"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteLeafCommandNoSuggestions(t *testing.T) {
	// docker ps — лист без обязательных параметров: подсказываем только help.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "ps"}, "")
	got := text(matches)
	want := []string{"help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteLeafCommandRequiredParams(t *testing.T) {
	// docker build: param tag (required), force (bool, not required).
	// На листовой команде без "--" показываем обязательные параметры + help.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build"}, "")
	got := text(matches)
	want := []string{"--force", "--tag", "help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteLeafRequiredParamHasDescription(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build"}, "")
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
	want := []string{"--force", "--tag"}
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
	// Значение после = — user-supplied путь: разрешаем файловое дополнение.
	if directive != ShellCompDirectiveDefault {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteAfterBoolFlagNoTrueFalse(t *testing.T) {
	// Парсер не принимает "--force true": после bool-флага подсказываем
	// оставшиеся флаги (как после завершённого флага), без true/false и help.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build", "--force"}, "")
	got := text(matches)
	want := []string{"--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteAfterFlagWithEqualsNoValueHint(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build", "--force=false"}, "")
	got := text(matches)
	want := []string{"--tag"}
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
	// Значение string-флага — user-supplied путь: разрешаем файлы.
	if directive != ShellCompDirectiveDefault {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteDoubleDashDefault(t *testing.T) {
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "ps", "--"}, "some")
	if len(matches) != 0 {
		t.Fatalf("expected no matches after --, got %v", matches)
	}
	if directive != ShellCompDirectiveDefault {
		t.Fatalf("unexpected directive: %v, want Default", directive)
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
	want := []string{"help", "staging"}
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
	want := []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--ref", "--skip-attach"}
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

func TestCompleteHelpShownAtModuleLevel(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker"}, "")
	got := text(matches)
	if !contains(got, "help") {
		t.Fatalf("expected help at module level, got %v", got)
	}
}

func TestCompleteHelpShownAtBranchLevel(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "migrate"}, "")
	got := text(matches)
	if !contains(got, "help") {
		t.Fatalf("expected help at branch level, got %v", got)
	}
}

func TestCompleteHelpShownAtLeafLevel(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "ps"}, "")
	got := text(matches)
	if !contains(got, "help") {
		t.Fatalf("expected help at leaf level, got %v", got)
	}
}

func TestCompleteHelpNotDuplicatedWithUserCommand(t *testing.T) {
	// У mod есть user-defined команда "help". Системная подсказка не должна
	// дублировать её — "help" присутствует ровно один раз.
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
	for _, toComplete := range []string{"", "--"} {
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
	// Модуль-однокоманда (cmd + params, без commands): на пустом
	// commandPath подсказываем параметры default-команды.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"meta"}, "")
	got := text(matches)
	want := []string{"--json", "help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}

	// После bool-флага --json — оставшиеся флаги, без true/false.
	matches, _ = Complete(src, nil, []string{"meta", "--json"}, "")
	got = text(matches)
	if len(got) != 0 {
		t.Fatalf("after bool flag matches: %v, want none", got)
	}
}

func TestCompleteAfterFlagValue(t *testing.T) {
	// После введённого значения флага (--out ~/qr.png <TAB>) слово-значение
	// не должно попадать в command path: подсказываем оставшиеся параметры.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build", "--tag", "v1.0"}, "")
	got := text(matches)
	want := []string{"--force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty toComplete: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}

	// То же в ветке "--": после значения флага остаются другие флаги.
	matches, _ = Complete(src, nil, []string{"docker", "build", "--tag", "v1.0"}, "--")
	got = text(matches)
	want = []string{"--force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("-- toComplete: %v, want %v", got, want)
	}
}

func TestCompleteFlagValueAllowsFiles(t *testing.T) {
	// Значение не-bool флага (user-supplied путь/строка): разрешаем шеллу
	// дефолтное файловое дополнение (директива Default). В будущем —
	// только для параметров типа path (ROADMAP в README).
	src := newTestSource(t)

	// Курсор на пустом значении: --out <TAB>
	matches, directive := Complete(src, nil, []string{"docker", "build", "--tag"}, "")
	if len(matches) != 0 {
		t.Fatalf("expected no matches, got %v", matches)
	}
	if directive != ShellCompDirectiveDefault {
		t.Fatalf("expected Default directive, got %v", directive)
	}

	// Курсор в середине значения: --out ~/qr<TAB>
	matches, directive = Complete(src, nil, []string{"docker", "build", "--tag"}, "~/qr")
	if len(matches) != 0 {
		t.Fatalf("expected no matches, got %v", matches)
	}
	if directive != ShellCompDirectiveDefault {
		t.Fatalf("expected Default directive, got %v", directive)
	}
}

func TestCompleteStringFlagValuePathStaysResolved(t *testing.T) {
	// futi docker build --tag myapp <TAB>: myapp — значение флага, путь
	// остаётся docker build; подсказываем оставшиеся флаги.
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build", "--tag", "myapp"}, "")
	got := text(matches)
	want := []string{"--force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
}

func TestCompleteBuiltinFlagsKeepBehavior(t *testing.T) {
	// Builtins с флагами (модуль не зарезолвен): поведение то же, что до F70.
	src := newTestSource(t)
	cases := []struct {
		args []string
		want Directive
	}{
		// после флага без "=" — значение user-supplied: файлы
		{[]string{"modules", "update", "--all"}, ShellCompDirectiveDefault},
		{[]string{"modules", "install", "x", "--local"}, ShellCompDirectiveDefault},
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

func TestCompleteAfterShortHelpFlag(t *testing.T) {
	// -h — флаговый токен: после него путь не растёт, подсказываем флаги команды.
	src := newTestSource(t)
	matches, directive := Complete(src, nil, []string{"docker", "build", "-h"}, "")
	got := text(matches)
	want := []string{"--force", "--tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected matches: %v, want %v", got, want)
	}
	if directive != ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
}

func TestCompleteExcludesUsedFlags(t *testing.T) {
	src := newTestSource(t)
	matches, _ := Complete(src, nil, []string{"docker", "build", "--tag=x", "--force"}, "")
	if len(matches) != 0 {
		t.Fatalf("expected no matches, got %v", text(matches))
	}
	matches, _ = Complete(src, nil, []string{"docker", "build", "--tag=x"}, "--")
	if got := text(matches); !reflect.DeepEqual(got, []string{"--force"}) {
		t.Fatalf("-- toComplete: %v, want [--force]", got)
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
	want := []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--ref", "--skip-attach"}
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
	want = []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--skip-attach"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install with --ref before URL: got %v, want %v", got, want)
	}

	// Случай 4: modules install с URL и --ref после (флаг уже введён)
	matches, _ = Complete(src, nil, []string{"modules", "install", "https://x/repo.git", "--ref", "v1"}, "--")
	got = text(matches)
	want = []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--skip-attach"}
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
		{"validate", false},
		{"help", false},
		{"init", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsReservedName(tt.name); got != tt.want {
			t.Errorf("IsReservedName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
	if len(ReservedNames) != 5 {
		t.Errorf("ReservedNames = %v, want 5 names", ReservedNames)
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
	want := []string{"cache", "completion", "dev", "list", "modules"}
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
	matches, directive := Complete(src, nil, []string{"list"}, "")
	if len(matches) != 0 {
		t.Fatalf("Complete([list]) = %v, want empty", text(matches))
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
