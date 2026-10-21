package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/completion"
	"github.com/tivjnb/futi/internal/core"
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

// testSourceBuilder создаёт GlobalSource над временной директорией с демо-модулями.
func testSourceBuilder(t *testing.T) discovery.Source {
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
`)
	writeFile(t, filepath.Join(dir, "git.yaml"), `
name: git
description: Git shortcuts
commands:
  commit:
    cmd: "git commit"
`)
	return discovery.NewGlobalSource(dir)
}

func TestRunCompleteOutputFormat(t *testing.T) {
	src := testSourceBuilder(t)
	var out, errBuf bytes.Buffer

	// args=["docker"], toComplete="" → команды модуля docker.
	err := runComplete(&out, &errBuf, src, nil, []string{"docker", ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("unexpected lines: %v", lines)
	}
	if lines[0] != "build\tBuild image" {
		t.Fatalf("unexpected first match: %q", lines[0])
	}
	if lines[len(lines)-1] != ":4" {
		t.Fatalf("unexpected directive line: %q, want :4", lines[len(lines)-1])
	}
	if !strings.Contains(errBuf.String(), "Completion ended with directive") {
		t.Fatalf("expected stderr debug message, got %q", errBuf.String())
	}
}

func TestRunCompleteDirectiveDefault(t *testing.T) {
	src := testSourceBuilder(t)
	var out bytes.Buffer

	// После "--" — directive Default (0), подсказок нет.
	err := runComplete(&out, nil, src, nil, []string{"docker", "ps", "--", "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.String() != ":0\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunCompleteNoMatches(t *testing.T) {
	src := testSourceBuilder(t)
	var out bytes.Buffer
	err := runComplete(&out, nil, src, nil, []string{"nope", ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.String() != ":4\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunCompleteMissingArg(t *testing.T) {
	src := testSourceBuilder(t)
	var out bytes.Buffer
	err := runComplete(&out, nil, src, nil, []string{})
	if err == nil {
		t.Fatal("expected error for missing toComplete")
	}
}

func TestRunCompletionShells(t *testing.T) {
	cases := []struct {
		shell  string
		marker string
	}{
		{completionBash, `complete -o default -F _futi futi`},
		{completionZsh, `#compdef futi`},
		{completionFish, `complete -c futi -f`},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			var out bytes.Buffer
			if err := runCompletion(&out, c.shell); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out.String(), "__complete") {
				t.Fatalf("script for %s missing __complete call", c.shell)
			}
			if !strings.Contains(out.String(), c.marker) {
				t.Fatalf("script for %s missing marker %q", c.shell, c.marker)
			}
		})
	}
}

func TestRunCompletionUnknownShell(t *testing.T) {
	var out bytes.Buffer
	err := runCompletion(&out, "zz")
	if err == nil {
		t.Fatal("expected error for unknown shell")
	}
	for _, s := range validCompletionShells {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error should list valid shell %q, got %v", s, err)
		}
	}
}

func TestCompletionNeedsRefreshNeverRefreshedEmpty(t *testing.T) {
	// Кэш, который никогда не обновлялся → нужен refresh.
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	if !completionNeedsRefresh(cache) {
		t.Fatal("expected refresh needed for never-refreshed cache")
	}
}

func TestCompletionNeedsRefreshNeverRefreshed(t *testing.T) {
	// Кэш с entries, но never-refreshed (LoadModule без Refresh) → нужен refresh.
	src := testSourceBuilder(t)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	// Загружаем реальный модуль из source напрямую (без Refresh).
	manifest, root, err := src.Resolve("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := cache.LoadModule(manifest, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !completionNeedsRefresh(cache) {
		t.Fatal("expected refresh needed for cache with entries but no last refresh")
	}
}

func TestCompletionNeedsRefreshFresh(t *testing.T) {
	// Кэш со свежим lastRefresh → refresh не нужен.
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	if _, err := cache.Refresh(testSourceBuilder(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completionNeedsRefresh(cache) {
		t.Fatal("expected no refresh needed for fresh cache")
	}
}

// TestCompletionNoRefreshAfterEmptyRefresh — регрессия F21: при пустом каталоге
// глобальных модулей первый __complete обновляет кэш, второй (новый Cache,
// Load с диска) — уже нет, иначе каждый TAB пишет файл кэша.
func TestCompletionNoRefreshAfterEmptyRefresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	opts := core.Options{
		ModulesDir: t.TempDir(),
		CachePath:  filepath.Join(t.TempDir(), "futi", "modulecache.gob"),
	}
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := runCompleteMain(&stdout, &stderr, rt, []string{""}); code != 0 {
		t.Fatalf("first runCompleteMain code = %d, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(opts.CachePath); err != nil {
		t.Fatalf("cache file should exist after first __complete: %v", err)
	}

	rt2, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	second := rt2.Cache()
	second.Load()
	if second.LastRefresh().IsZero() {
		t.Fatal("LastRefresh should survive Save/Load after refresh of empty modules dir")
	}
	if completionNeedsRefresh(second) {
		t.Fatal("second __complete must not refresh cache again")
	}
}

// TestCompletionFishNoEval — регрессионный тест REV-003: fish-скрипт не должен
// использовать eval для вызова __complete. eval парсит склеенную из токенов
// commandline строку как fish-код: $() и ';' из набранной пользователем строки
// исполняются уже при TAB, пробельные токены распадаются. Прямой вызов
// ($args[1] __complete $args[2..-1]) передаёт токены как отдельные argv без
// повторного парсинга (поведение подтверждено на fish 3.7.0).
func TestCompletionFishNoEval(t *testing.T) {
	if strings.Contains(fishCompletionScript, "eval") {
		t.Fatal("fish completion script must not use eval (REV-003 regression)")
	}
	if !strings.Contains(fishCompletionScript, "($args[1] __complete $args[2..-1]") {
		t.Fatal("fish completion script missing direct __complete call without eval")
	}
}

func TestSanitizeCompletionTextStripsEscape(t *testing.T) {
	if got, want := sanitizeCompletionText("a\x1b[31mb\tc"), "a[31mb c"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunCompleteSanitizesSpecialChars проверяет санитизацию кандидатов
// (REV-010): управляющие символы в тексте/описании (\t, \n, \r) заменяются
// на пробел — иначе \n разлагает кандидата на строки и ломает построчный
// протокол __complete, а \t искажает формат "text\tdescription".
func TestRunCompleteSanitizesSpecialChars(t *testing.T) {
	dir := t.TempDir()
	// YAML double-quoted scalar разворачивает \n/\t/\r в управляющие символы.
	writeFile(t, filepath.Join(dir, "weird.yaml"), "name: weird\ndescription: \"line1\\nline2\\twith\\ttabs\\r\"\ncmd: \"echo hi\"\n")
	writeFile(t, filepath.Join(dir, "normal.yaml"), "name: normal\ndescription: Normal module\ncmd: \"echo ok\"\n")
	src := discovery.NewGlobalSource(dir)

	var out bytes.Buffer
	err := runComplete(&out, nil, src, nil, []string{""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 4 builtin (cache/completion/list/modules) + normal + weird + directive.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected 7 lines (4 builtins + 2 modules + directive), got %d: %q", len(lines), out.String())
	}
	for _, ln := range lines {
		if strings.Contains(ln, "\r") {
			t.Fatalf("line %q contains CR", ln)
		}
		// Ровно один таб-разделитель "text\tdescription" допустим; второй
		// таб означает несанитизированный \t внутри текста/описания.
		if strings.Count(ln, "\t") > 1 {
			t.Fatalf("line %q contains tab inside text/description", ln)
		}
	}
	// Многострочное описание склеено пробелами в одну строку.
	found := false
	for _, ln := range lines {
		if ln == "weird\tline1 line2 with tabs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sanitized line %q, got %q", "weird\tline1 line2 with tabs", out.String())
	}
}

// fishPassesCursorRe находит безусловную строку set args $args "$cursor"
// независимо от отступа и перевода строки.
var fishPassesCursorRe = regexp.MustCompile(`(?m)^\s*set args \$args "\$cursor"\s*$`)

// TestCompletionFishAlwaysPassesCursor: слово под курсором (commandline -ct)
// всегда уходит в __complete последним аргументом, иначе futi do<TAB> вызывает
// __complete без аргументов (audit F20).
func TestCompletionFishAlwaysPassesCursor(t *testing.T) {
	if !fishPassesCursorRe.MatchString(fishCompletionScript) {
		t.Error("скрипт не содержит безусловного set args $args \"$cursor\"")
	}
	if strings.Contains(fishCompletionScript, `if test -z "$cursor"`) {
		t.Error("скрипт добавляет курсор только для пустого слова")
	}
}

// TestCompletionFishRealShell прогоняет скрипт в настоящем fish; без fish пропускается.
func TestCompletionFishRealShell(t *testing.T) {
	fishPath, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish не установлен")
	}
	tmp := t.TempDir()
	binDir := filepath.Dir(builtFutiBinary(t))
	modules := filepath.Join(tmp, "modules")
	writeFile(t, filepath.Join(modules, "docker.yaml"), "name: docker\ndescription: Docker management\ncommands:\n  build:\n    description: Build\n    cmd: echo build\n")

	cmd := exec.Command(fishPath, "--no-config", "-c", "futi completion fish | source; complete -C 'futi do'")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FUTI_MODULES_DIR="+modules,
		"HOME="+tmp,
		"XDG_CACHE_HOME="+filepath.Join(tmp, "cache"),
		"XDG_CONFIG_HOME="+filepath.Join(tmp, "config"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fish: %v\n%s", err, out)
	}
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if name == "docker" {
			found = true
		}
	}
	if !found {
		t.Errorf("вывод fish не содержит строки docker: %q", out)
	}
}

// TestCompletionModulesFlagsAcceptedByParsers: каждый флаг, который completion
// подсказывает для `futi modules <sub>`, принимается парсером подкоманды
// (защита от рассинхрона списков и парсеров).
func TestCompletionModulesFlagsAcceptedByParsers(t *testing.T) {
	parsers := map[string]func([]string) error{
		"add_command": func(a []string) error { _, err := parseAddCommandArgs(a); return err },
		"init":        func(a []string) error { _, err := parseInitArgs(a); return err },
		"install":     func(a []string) error { _, err := parseInstallArgs(a); return err },
		"uninstall":   func(a []string) error { _, err := parseUninstallArgs(a); return err },
		"update":      func(a []string) error { _, err := parseUpdateArgs(a); return err },
	}
	for _, sub := range completion.ModulesSubcommands() {
		flags := completion.ModulesFlags(sub)
		parse, hasParser := parsers[sub]
		if flags == nil {
			if hasParser {
				t.Errorf("%s: parser registered, but completion suggests no flags", sub)
			}
			continue
		}
		if !hasParser {
			t.Errorf("%s: completion suggests flags %v, but no parser is registered in the test", sub, flags)
			continue
		}
		valueFlags := make(map[string]bool)
		for _, f := range completion.ModulesValueFlags(sub) {
			valueFlags[f] = true
		}
		for _, flag := range flags {
			// Разбиение на булевы и со значением совпадает с парсером: флаг со
			// значением без значения — ошибка "requires a value", булев — нет.
			err := parse([]string{"x", flag})
			needsValue := err != nil && strings.Contains(err.Error(), "requires a value")
			if needsValue != valueFlags[flag] {
				t.Errorf("%s: flag %s: completion value-flag=%v, parser requires value=%v (err=%v)", sub, flag, valueFlags[flag], needsValue, err)
			}
			// Значение и позиционный аргумент: флаг с аргументом забирает "val",
			// булев оставляет его позиционным — «unknown flag» бывает только у
			// флага, которого парсер не знает.
			for _, args := range [][]string{{"x", flag}, {"x", flag, "val"}} {
				if err := parse(args); err != nil && strings.Contains(err.Error(), "unknown flag") {
					t.Errorf("%s: flag %s suggested by completion, parser rejects args %v: %v", sub, flag, args, err)
				}
			}
		}
	}
}
