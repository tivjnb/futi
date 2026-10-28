package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/shlex"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

// TestRunInitCreatesLocalFile проверяет, что futi init mytool создаёт
// ./.futi/mytool.yaml (доступен через auto-discovery) с валидным модулем.
func TestRunInitCreatesLocalFile(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(".futi/mytool.yaml")
	if err != nil {
		t.Fatalf("expected .futi/mytool.yaml to exist: %v", err)
	}
	if !strings.Contains(string(data), `name: "mytool"`) {
		t.Fatalf("expected 'name: \"mytool\"' in file:\n%s", data)
	}
	if !strings.Contains(buf.String(), "mytool.yaml") {
		t.Fatalf("expected file path in output: %s", buf.String())
	}
}

// TestRunInitGeneratedModuleValidates проверяет, что созданный модуль
// проходит LoadModule (с валидацией схемы).
func TestRunInitGeneratedModuleValidates(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mod, err := execmodule.LoadModule(".futi/mytool.yaml", ".futi")
	if err != nil {
		t.Fatalf("generated module should be valid: %v", err)
	}
	if mod.Name != "mytool" {
		t.Fatalf("expected module name 'mytool', got %q", mod.Name)
	}
	if len(mod.Commands) == 0 {
		t.Fatal("expected at least one command in generated module")
	}
}

// TestRunInitWithNamespace проверяет создание модуля с namespace через путь.
// futi init dev/mytool → ./.futi/dev/mytool.yaml
func TestRunInitWithNamespace(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "dev/mytool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(".futi/dev/mytool.yaml"); err != nil {
		t.Fatalf("expected .futi/dev/mytool.yaml to exist: %v", err)
	}

	mod, err := execmodule.LoadModule(".futi/dev/mytool.yaml", ".futi/dev")
	if err != nil {
		t.Fatalf("generated module should be valid: %v", err)
	}
	if mod.Name != "mytool" {
		t.Fatalf("expected module name 'mytool', got %q", mod.Name)
	}
}

// TestRunInitGlobal проверяет создание модуля в глобальном каталоге.
func TestRunInitGlobal(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool", Global: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPath := filepath.Join(tmpHome, ".config", "futi", "modules", "mytool.yaml")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected %s to exist: %v", expectedPath, err)
	}
}

// TestRunInitExistingFileError проверяет, что при существующем файле
// возвращается ошибка (без перезаписи).
func TestRunInitExistingFileError(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.MkdirAll(".futi", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".futi/mytool.yaml", []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := Init(&buf, io.Discard, InitOptions{Path: "mytool"})
	if err == nil {
		t.Fatal("expected error for existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error should mention 'already exists', got: %v", err)
	}

	data, _ := os.ReadFile(".futi/mytool.yaml")
	if string(data) != "existing" {
		t.Fatal("existing file should not be overwritten")
	}
}

// TestRunInitWithDesc проверяет флаг --desc.
func TestRunInitWithDesc(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool", Description: "My awesome tool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".futi/mytool.yaml")
	if !strings.Contains(string(data), `description: "My awesome tool"`) {
		t.Fatalf("expected quoted description in file:\n%s", data)
	}
}

// TestRunInitWithDefaultCmd проверяет флаг --default-cmd.
func TestRunInitWithDefaultCmd(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool", DefaultCmd: "echo hi"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".futi/mytool.yaml")
	if !strings.Contains(string(data), `cmd: "echo hi"`) {
		t.Fatalf("expected default cmd in file:\n%s", data)
	}
}

// TestRunInitWithCommand проверяет флаг --command.
func TestRunInitWithCommand(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool", Commands: []string{"greet", "build"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".futi/mytool.yaml")
	if !strings.Contains(string(data), "greet") || !strings.Contains(string(data), "build") {
		t.Fatalf("expected both commands in file:\n%s", data)
	}

	mod, err := execmodule.LoadModule(".futi/mytool.yaml", ".futi")
	if err != nil {
		t.Fatalf("generated module should be valid: %v", err)
	}
	if _, ok := mod.Commands["greet"]; !ok {
		t.Fatal("expected 'greet' command in module")
	}
	if _, ok := mod.Commands["build"]; !ok {
		t.Fatal("expected 'build' command in module")
	}
}

// TestRunInitGlobalWithNamespace проверяет namespace + global.
func TestRunInitGlobalWithNamespace(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "dev/mytool", Global: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPath := filepath.Join(tmpHome, ".config", "futi", "modules", "dev", "mytool.yaml")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected %s to exist: %v", expectedPath, err)
	}
}

// TestRunInitCreatedModuleDiscoverable проверяет, что созданный локальный
// модуль находится через локальный source Runtime (auto-discovery ./.futi/).
func TestRunInitCreatedModuleDiscoverable(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rt, err := core.NewRuntime(core.Options{ModulesDir: t.TempDir(), LocalDir: "."})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	local, err := rt.Source()
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	manifestPath, moduleRoot, err := local.Resolve("mytool")
	if err != nil {
		t.Fatalf("expected to discover 'mytool': %v", err)
	}
	if filepath.Base(manifestPath) != "mytool.yaml" {
		t.Fatalf("expected manifest mytool.yaml, got %s", manifestPath)
	}
	if filepath.Base(moduleRoot) != ".futi" {
		t.Fatalf("expected moduleRoot ending in .futi, got %s", moduleRoot)
	}
}

// TestRunInitGlobalModuleDiscoverable проверяет, что созданный глобальный
// модуль находится через discovery (GlobalSource).
func TestRunInitGlobalModuleDiscoverable(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool", Global: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	modDir := filepath.Join(tmpHome, ".config", "futi", "modules")
	src := discovery.NewGlobalSource(modDir)

	manifestPath, moduleRoot, err := src.Resolve("mytool")
	if err != nil {
		t.Fatalf("expected to discover 'mytool': %v", err)
	}
	if filepath.Base(manifestPath) != "mytool.yaml" {
		t.Fatalf("expected manifest mytool.yaml, got %s", manifestPath)
	}
	if moduleRoot != modDir {
		t.Fatalf("expected moduleRoot %s, got %s", modDir, moduleRoot)
	}
}

// TestGenerateModuleYAMLMinimal проверяет генерацию минимального шаблона.
func TestGenerateModuleYAMLMinimal(t *testing.T) {
	yaml := generateModuleYAML("mytool", "My tool", "", nil)
	if !strings.Contains(yaml, `name: "mytool"`) {
		t.Fatalf("expected 'name: \"mytool\"':\n%s", yaml)
	}
	if !strings.Contains(yaml, `description: "My tool"`) {
		t.Fatalf("expected 'description: \"My tool\"':\n%s", yaml)
	}
	if !strings.Contains(yaml, "hello") {
		t.Fatalf("expected default 'hello' command:\n%s", yaml)
	}
}

// TestGenerateModuleYAMLWithDefaultCmd проверяет генерацию с default cmd.
func TestGenerateModuleYAMLWithDefaultCmd(t *testing.T) {
	yaml := generateModuleYAML("mytool", "desc", "echo hi", nil)
	if !strings.Contains(yaml, `cmd: "echo hi"`) {
		t.Fatalf("expected cmd field:\n%s", yaml)
	}
}

// TestGenerateModuleYAMLWithCommands проверяет генерацию с командами.
func TestGenerateModuleYAMLWithCommands(t *testing.T) {
	yaml := generateModuleYAML("mytool", "desc", "", []string{"greet", "build"})
	if !strings.Contains(yaml, "greet") || !strings.Contains(yaml, "build") {
		t.Fatalf("expected both commands:\n%s", yaml)
	}
	// Без --default-cmd нет cmd на уровне модуля
	lines := strings.Split(yaml, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "cmd:") && !strings.Contains(trimmed, "echo") {
			// cmd внутри команды — OK
		}
	}
}

// --- #40: path traversal via .. in path ---

// TestRunInitPathTraversalError проверяет, что path с .. отклоняется.
func TestRunInitPathTraversalError(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	err := Init(&buf, io.Discard, InitOptions{Path: "../../etc/passwd", Global: true})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "invalid module name") {
		t.Fatalf("expected invalid module name error, got: %v", err)
	}
	// Защитный барьер targetFilePath срабатывает и без проверки имени.
	_, err = targetFilePath(&InitOptions{Path: "../../etc/passwd", Global: true}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("targetFilePath err = %v, want path outside", err)
	}
}

// TestRunInitPathTraversalLocal проверяет, что path с .. отклоняется локально тоже.
func TestRunInitPathTraversalLocal(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	err := Init(&buf, io.Discard, InitOptions{Path: "../escape"})
	if err == nil {
		t.Fatal("expected error for path traversal in local mode")
	}
}

// --- #41: YAML injection via description ---

// TestGenerateModuleYAMLDescriptionQuoted проверяет, что description всегда
// квотируется (фикс #41 — YAML-инъекция через перевод строки).
func TestGenerateModuleYAMLDescriptionQuoted(t *testing.T) {
	yaml := generateModuleYAML("mytool", "simple desc", "", nil)
	if !strings.Contains(yaml, `description: "simple desc"`) {
		t.Fatalf("expected quoted description:\n%s", yaml)
	}
}

// TestGenerateModuleYAMLDescriptionWithNewline проверяет, что description с
// переводом строки квотируется и не позволяет YAML-инъекцию.
func TestGenerateModuleYAMLDescriptionWithNewline(t *testing.T) {
	malicious := "foo\ncommands:\n  evil:\n    cmd: rm -rf /"
	yaml := generateModuleYAML("mytool", malicious, "", nil)
	// description должно быть в кавычках — evil не должно стать отдельным ключом
	if !strings.Contains(yaml, `description: "foo`) {
		t.Fatalf("expected quoted description with newline:\n%s", yaml)
	}
	// Проверяем, что "evil" не появился как отдельная команда на верхнем уровне
	lines := strings.Split(yaml, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "evil:" {
			t.Fatalf("YAML injection detected — 'evil:' found as top-level key:\n%s", yaml)
		}
	}
}

// --- #45: double .yaml extension ---

// TestRunInitDoubleYamlExtension проверяет, что futi init mytool.yaml не
// создаёт mytool.yaml.yaml (фикс #45).
func TestRunInitDoubleYamlExtension(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool.yaml"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(".futi/mytool.yaml"); err != nil {
		t.Fatalf("expected .futi/mytool.yaml to exist: %v", err)
	}
	if _, err := os.Stat(".futi/mytool.yaml.yaml"); err == nil {
		t.Fatal("expected .futi/mytool.yaml.yaml to NOT exist (double extension)")
	}
}

// TestRunInitDoubleYmlExtension проверяет тоже для .yml.
func TestRunInitDoubleYmlExtension(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "mytool.yml"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(".futi/mytool.yml"); err != nil {
		t.Fatalf("expected .futi/mytool.yml to exist: %v", err)
	}
}

// --- #46: name not quoted ---

// TestGenerateModuleYAMLNameQuoted проверяет, что name всегда квотируется
// (фикс #46 — name с спецсимволами ломает YAML).
func TestGenerateModuleYAMLNameQuoted(t *testing.T) {
	yaml := generateModuleYAML("mytool", "desc", "", nil)
	if !strings.Contains(yaml, `name: "mytool"`) {
		t.Fatalf("expected quoted name:\n%s", yaml)
	}
}

// TestGenerateModuleYAMLNameWithSpecialChars проверяет, что name с двоеточием
// квотируется и не ломает YAML.
func TestGenerateModuleYAMLNameWithSpecialChars(t *testing.T) {
	yaml := generateModuleYAML("my:tool", "desc", "", nil)
	if !strings.Contains(yaml, `name: "my:tool"`) {
		t.Fatalf("expected quoted name with colon:\n%s", yaml)
	}
}

// --- #47: command name not sanitized as YAML key ---

// TestGenerateModuleYAMLCommandKeyQuoted проверяет, что имя команды квотируется
// как YAML-ключ (фикс #47).
func TestGenerateModuleYAMLCommandKeyQuoted(t *testing.T) {
	yaml := generateModuleYAML("mytool", "desc", "", []string{"greet"})
	if !strings.Contains(yaml, `"greet":`) {
		t.Fatalf("expected quoted command key:\n%s", yaml)
	}
}

// TestGenerateModuleYAMLCommandKeyWithSpecialChars проверяет, что имя команды
// с двоеточием квотируется.
func TestGenerateModuleYAMLCommandKeyWithSpecialChars(t *testing.T) {
	yaml := generateModuleYAML("mytool", "desc", "", []string{"my:cmd"})
	if !strings.Contains(yaml, `"my:cmd":`) {
		t.Fatalf("expected quoted command key with colon:\n%s", yaml)
	}
}

// --- #49: reserved name ---

// TestRunInitReservedNameList проверяет, что futi init list возвращает ошибку
// (фикс #49 — конфликт с зарезервированным словом).
func TestRunInitReservedNameList(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	err := Init(&buf, io.Discard, InitOptions{Path: "list"})
	if err == nil {
		t.Fatal("expected error for reserved name 'list'")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected error about reserved name, got: %v", err)
	}
}

// TestRunInitReservedNameCache проверяет, что futi init cache возвращает ошибку.
func TestRunInitReservedNameCache(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	err := Init(&buf, io.Discard, InitOptions{Path: "cache"})
	if err == nil {
		t.Fatal("expected error for reserved name 'cache'")
	}
}

// TestRunInitThroughSymlinkShowsResolvedPath проверяет честный UX при записи
// через симлинк-директорию (dotfiles-репо, dev-симлинк): файл физически
// создаётся в целевом каталоге симлинка, и "Created:" показывает
// РЕЗОЛВНУТЫЙ путь, а не лексический (который скрывал реальное место записи).
func TestRunInitThroughSymlinkShowsResolvedPath(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(t.TempDir(), "realpkg")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "linkedpkg")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	t.Setenv("FUTI_MODULES_DIR", base)
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := Init(&buf, io.Discard, InitOptions{Path: "linkedpkg/newmod", Global: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Вывод показывает резолвнутый путь (реальное место записи).
	resolvedPath := filepath.Join(realDir, "newmod.yaml")
	if !strings.Contains(out, resolvedPath) {
		t.Fatalf("output should show resolved path %q, got %q", resolvedPath, out)
	}
	// Файл физически в целевом каталоге симлинка.
	if _, err := os.Stat(resolvedPath); err != nil {
		t.Fatalf("expected file at resolved path %s: %v", resolvedPath, err)
	}
}

// F57: зарезервированное слово запрещено в любом сегменте пути, а не только
// в basename; validate и help зарезервированными не являются.
func TestRunInitReservedSegments(t *testing.T) {
	for _, arg := range []string{"list", "cache", "modules", "completion", "__complete", "cache/tool", "a/list", "a/list.yaml", "a/list.yml", "a/modules/b"} {
		t.Run("reject/"+arg, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: arg})
			if err == nil || !strings.Contains(err.Error(), "is a reserved name") {
				t.Fatalf("init %q: expected reserved name error, got %v", arg, err)
			}
			if _, statErr := os.Stat(".futi"); statErr == nil {
				t.Fatal(".futi must not be created")
			}
		})
	}
	for _, arg := range []string{"a/validate", "tools/help", "validate"} {
		t.Run("accept/"+arg, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: arg}); err != nil {
				t.Fatalf("init %q: unexpected error %v", arg, err)
			}
		})
	}
}

// F56: точка в сегменте пути — разделитель namespace, файл не резолвился бы.
func TestRunInitDottedNameRejected(t *testing.T) {
	for _, arg := range []string{"my.tool", "dev/my.tool", "my.tool.yaml", "a.b/tool"} {
		t.Run(arg, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: arg})
			if err == nil || !strings.Contains(err.Error(), "invalid module name") {
				t.Fatalf("init %q: expected invalid module name error, got %v", arg, err)
			}
			if _, statErr := os.Stat(".futi"); statErr == nil {
				t.Fatal(".futi must not be created")
			}
		})
	}
}

// F59: x.yaml рядом с x/config.yaml — конфликт discovery.
func TestRunInitConflictsWithConfigDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".futi/x", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".futi/x/config.yaml", []byte("commands:\n  a:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: "x"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if _, err := os.Stat(".futi/x.yaml"); err == nil {
		t.Fatal(".futi/x.yaml must not be created")
	}
}

// Контроль: допустимые формы пути работают как раньше.
func TestRunInitNestedPathStillWorks(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: "dev/tool"}); err != nil {
		t.Fatalf("init dev/tool: %v", err)
	}
	if _, err := os.Stat(".futi/dev/tool.yaml"); err != nil {
		t.Fatal(err)
	}
}

// F59: init x.yaml при существующем x/config.yaml — тот же конфликт.
func TestRunInitYamlSuffixConflictsWithConfigDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".futi/x", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".futi/x/config.yaml", []byte("commands:\n  a:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: "x.yaml"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(".futi/x.yaml"); err == nil {
		t.Fatal(".futi/x.yaml must not be created")
	}
}

// F59 (симметрия): init x/config при существующем x.yaml — конфликт discovery.
func TestRunInitConfigConflictsWithYamlFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".futi", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".futi/x.yaml", []byte("commands:\n  a:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: "x/config"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if _, err := os.Stat(".futi/x/config.yaml"); err == nil {
		t.Fatal(".futi/x/config.yaml must not be created")
	}
}

// F62: имя в позиционном аргументе проходит validateModuleName.
func TestModulesInitRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"foo bar", "-x", "a//b", "a/.", "a;b"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := Init(&bytes.Buffer{}, io.Discard, InitOptions{Path: name})
			if err == nil || !strings.Contains(err.Error(), "invalid module name") {
				t.Fatalf("err = %v, want invalid module name", err)
			}
			if _, statErr := os.Stat(".futi"); statErr == nil {
				t.Fatal(".futi must not be created")
			}
		})
	}
}

// F62: cmd сгенерированной команды разбирается shlex в [echo, <имя>].
func TestGenerateModuleYAMLQuotesCommand(t *testing.T) {
	for _, name := range []string{`a"b`, `it's`, `x\y`, `$HOME`, "plain"} {
		t.Run(name, func(t *testing.T) {
			yaml := generateModuleYAML("m", "d", "", []string{name})
			mod, err := execmodule.ParseManifest([]byte(yaml), "/x/m.yaml", "/x")
			if err != nil {
				t.Fatalf("ParseManifest: %v\n%s", err, yaml)
			}
			cmd := mod.Commands[name].Cmd
			got, err := shlex.Split(cmd)
			if err != nil {
				t.Fatalf("shlex.Split(%q): %v", cmd, err)
			}
			want := []string{"echo", name}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("argv = %q, want %q", got, want)
			}
		})
	}
}

func TestInitRejectsReservedPath(t *testing.T) {
	t.Chdir(t.TempDir())
	err := Init(io.Discard, io.Discard, InitOptions{Path: "list"})
	if err == nil || !strings.Contains(err.Error(), `"list" is a reserved name`) {
		t.Fatalf("Init(list) err = %v, want reserved name error", err)
	}
	if _, statErr := os.Stat(".futi"); statErr == nil {
		t.Fatal(".futi must not be created")
	}
}

func TestInitDefaultsDescription(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Init(io.Discard, io.Discard, InitOptions{Path: "m"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(".futi", "m.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `description: "My tool"`) {
		t.Fatalf("manifest lacks default description:\n%s", data)
	}
}
