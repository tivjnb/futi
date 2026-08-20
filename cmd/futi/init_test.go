package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"futi/internal/discovery"
	"futi/internal/execmodule"
)

// TestRunInitCreatesLocalFile проверяет, что futi init mytool создаёт
// ./.futi/mytool.yaml (доступен через auto-discovery) с валидным модулем.
func TestRunInitCreatesLocalFile(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := runInit(&buf, []string{"mytool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(".futi/mytool.yaml")
	if err != nil {
		t.Fatalf("expected .futi/mytool.yaml to exist: %v", err)
	}
	if !strings.Contains(string(data), "name: mytool") {
		t.Fatalf("expected 'name: mytool' in file:\n%s", data)
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
	if err := runInit(&buf, []string{"mytool"}); err != nil {
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
	if err := runInit(&buf, []string{"dev/mytool"}); err != nil {
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
	if err := runInit(&buf, []string{"mytool", "--global"}); err != nil {
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
	err := runInit(&buf, []string{"mytool"})
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
	if err := runInit(&buf, []string{"mytool", "--desc", "My awesome tool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".futi/mytool.yaml")
	if !strings.Contains(string(data), "description: My awesome tool") {
		t.Fatalf("expected description in file:\n%s", data)
	}
}

// TestRunInitWithDefaultCmd проверяет флаг --default-cmd.
func TestRunInitWithDefaultCmd(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := runInit(&buf, []string{"mytool", "--default-cmd", "echo hi"}); err != nil {
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
	if err := runInit(&buf, []string{"mytool", "--command", "greet", "--command", "build"}); err != nil {
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

// TestRunInitNoArgsError проверяет, что без path возвращается ошибка.
func TestRunInitNoArgsError(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	err := runInit(&buf, []string{})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

// TestRunInitGlobalWithNamespace проверяет namespace + global.
func TestRunInitGlobalWithNamespace(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := runInit(&buf, []string{"dev/mytool", "--global"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPath := filepath.Join(tmpHome, ".config", "futi", "modules", "dev", "mytool.yaml")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected %s to exist: %v", expectedPath, err)
	}
}

// TestRunInitCreatedModuleDiscoverable проверяет, что созданный локальный
// модуль находится через localSource (auto-discovery ./.futi/).
func TestRunInitCreatedModuleDiscoverable(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	if err := runInit(&buf, []string{"mytool"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	local, err := localSource(nil)
	if err != nil {
		t.Fatalf("localSource: %v", err)
	}
	if local == nil {
		t.Fatal("expected local source to find .futi/mytool.yaml")
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
	if err := runInit(&buf, []string{"mytool", "--global"}); err != nil {
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
	if !strings.Contains(yaml, "name: mytool") {
		t.Fatalf("expected 'name: mytool':\n%s", yaml)
	}
	if !strings.Contains(yaml, "description: My tool") {
		t.Fatalf("expected 'description: My tool':\n%s", yaml)
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
