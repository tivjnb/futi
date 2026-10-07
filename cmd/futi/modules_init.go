package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/execmodule"
)

// initConfig — параметры подкоманды futi modules init.
type initConfig struct {
	path        string   // путь модуля (может содержать / для namespace)
	global      bool     // создавать в ~/.config/futi/modules/ вместо CWD
	description string   // описание модуля
	defaultCmd  string   // cmd на уровне модуля
	commands    []string // имена команд для шаблона
}

// parseInitArgs разбирает аргументы подкоманды futi modules init.
// Позиционный аргумент — path (обязательный). Флаги:
// --global, --desc <value>, --default-cmd <value>, --command <name> (repeatable).
// Флаги, требующие значение (--desc, --default-cmd, --command), отклоняют
// значения, начинающиеся с -- (фикс #48).
func parseInitArgs(args []string) (*initConfig, error) {
	cfg := &initConfig{}
	var positional []string

	// requireValue извлекает значение для флага и проверяет, что оно не
	// начинается с -- (фикс #48 — --desc --global не должно захватывать --global).
	requireValue := func(flagName string, i int) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flagName)
		}
		val := args[i+1]
		if strings.HasPrefix(val, "--") {
			return "", fmt.Errorf("%s requires a value, got flag %s", flagName, val)
		}
		return val, nil
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--global":
			cfg.global = true
		case arg == "--desc":
			val, err := requireValue("--desc", i)
			if err != nil {
				return nil, err
			}
			cfg.description = val
			i++
		case strings.HasPrefix(arg, "--desc="):
			cfg.description = strings.TrimPrefix(arg, "--desc=")
		case arg == "--default-cmd":
			val, err := requireValue("--default-cmd", i)
			if err != nil {
				return nil, err
			}
			cfg.defaultCmd = val
			i++
		case strings.HasPrefix(arg, "--default-cmd="):
			cfg.defaultCmd = strings.TrimPrefix(arg, "--default-cmd=")
		case arg == "--command":
			val, err := requireValue("--command", i)
			if err != nil {
				return nil, err
			}
			cfg.commands = append(cfg.commands, val)
			i++
		case strings.HasPrefix(arg, "--command="):
			cfg.commands = append(cfg.commands, strings.TrimPrefix(arg, "--command="))
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) == 0 {
		return nil, fmt.Errorf("usage: futi modules init <name> [flags]")
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one path argument, got %d", len(positional))
	}
	cfg.path = positional[0]

	// F57: зарезервированное слово запрещено в любом сегменте пути.
	trimmed := cfg.path
	if strings.HasSuffix(trimmed, ".yaml") {
		trimmed = strings.TrimSuffix(trimmed, ".yaml")
	} else {
		trimmed = strings.TrimSuffix(trimmed, ".yml")
	}
	if err := checkReservedSegments(filepath.ToSlash(trimmed), "/"); err != nil {
		return nil, err
	}

	if cfg.description == "" {
		cfg.description = "My tool"
	}

	return cfg, nil
}

// targetFilePath возвращает путь к файлу модуля.
// Локально: ./.futi/<path>.yaml (доступен через auto-discovery).
// Глобально: ~/.config/futi/modules/<path>.yaml.
// #40: отклоняет path с .. (path traversal за пределы base).
// #45: не добавляет .yaml, если path уже заканчивается на .yaml/.yml.
func targetFilePath(cfg *initConfig) (string, error) {
	base := ".futi"
	if cfg.global {
		modDir := os.Getenv("FUTI_MODULES_DIR")
		if modDir == "" {
			dir, err := config.DefaultModulesDir()
			if err != nil {
				return "", fmt.Errorf("modules dir: %w", err)
			}
			modDir = dir
		}
		base = modDir
	}

	// #45: не добавляем .yaml, если уже есть расширение.
	path := cfg.path
	if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
		path += ".yaml"
	}

	fullPath := filepath.Join(base, path)

	// #40: проверяем, что результат внутри base (нет path traversal).
	rel, err := filepath.Rel(base, fullPath)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q is outside the modules directory", cfg.path)
	}
	if err := validateSegmentsNoDot(strings.TrimSuffix(strings.TrimSuffix(rel, ".yaml"), ".yml")); err != nil {
		return "", err
	}
	// discovery видит и x.yaml, и x/config.yaml: оба сразу дали бы конфликт.
	if strings.HasSuffix(fullPath, ".yaml") {
		dirConfig := filepath.Join(strings.TrimSuffix(fullPath, ".yaml"), "config.yaml")
		if _, err := os.Stat(dirConfig); err == nil {
			return "", fmt.Errorf("module %q already exists at %s", cfg.path, dirConfig)
		}
	}
	// Симметрично: x/config.yaml при существующем x.yaml.
	if filepath.Base(fullPath) == "config.yaml" && filepath.Dir(fullPath) != base {
		siblingYAML := filepath.Dir(fullPath) + ".yaml"
		if _, err := os.Stat(siblingYAML); err == nil {
			return "", fmt.Errorf("module %q already exists at %s", cfg.path, siblingYAML)
		}
	}

	return fullPath, nil
}

// generateModuleYAML генерирует YAML-шаблон модуля.
// name — имя модуля (из последнего сегмента path).
// description — описание (квотируется для безопасности — фикс #41).
// defaultCmd — cmd на уровне модуля (пусто = не добавлять).
// commands — имена команд (если пусто — одна команда hello по умолчанию).
// #46: name квотируется. #47: ключи команд квотируются.
func generateModuleYAML(name, description, defaultCmd string, commands []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", quoteYAMLValue(name))
	fmt.Fprintf(&b, "description: %s\n", quoteYAMLValue(description))
	if defaultCmd != "" {
		fmt.Fprintf(&b, "cmd: %s\n", quoteYAMLValue(defaultCmd))
	}
	b.WriteString("commands:\n")
	if len(commands) > 0 {
		for _, cmd := range commands {
			fmt.Fprintf(&b, "  %s:\n    cmd: %s\n", quoteYAMLValue(cmd), quoteYAMLValue("echo "+cmd))
		}
	} else {
		b.WriteString("  \"hello\":\n    cmd: \"echo hello\"\n")
	}
	return b.String()
}

// quoteYAMLValue оборачивает значение в двойные кавычки для YAML.
// Использует Go-экранирование (%q), которое совместимо с YAML double-quoted style.
func quoteYAMLValue(s string) string {
	return fmt.Sprintf("%q", s)
}

// runModulesInit создаёт новый модуль с шаблоном YAML
// (подкоманда `futi modules init`).
// Парсит path + флаги, генерирует шаблон, пишет файл.
// Возвращает ошибку при существующем файле (без перезаписи).
// #44: использует O_EXCL вместо Stat+WriteFile для атомарной проверки.
func runModulesInit(w io.Writer, args []string) error {
	cfg, err := parseInitArgs(args)
	if err != nil {
		return err
	}

	targetPath, err := targetFilePath(cfg)
	if err != nil {
		return err
	}

	name := filepath.Base(cfg.path)
	name = strings.TrimSuffix(name, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	yaml := generateModuleYAML(name, cfg.description, cfg.defaultCmd, cfg.commands)

	// Сгенерированный манифест проверяется правилами схемы до записи:
	// иначе init сообщит об успехе, а модуль не загрузится.
	mod, err := execmodule.ParseManifest([]byte(yaml), targetPath, filepath.Dir(targetPath))
	if err != nil {
		return fmt.Errorf("invalid module: %w", err)
	}
	if errs, _ := mod.Validate(); len(errs) > 0 {
		return fmt.Errorf("invalid module: %w", errs[0])
	}

	dir := filepath.Dir(targetPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	// #44: O_EXCL — атомарная проверка существования без TOCTOU race.
	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists", targetPath)
		}
		return fmt.Errorf("create file: %w", err)
	}
	if _, err := f.Write([]byte(yaml)); err != nil {
		f.Close()
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	// Показываем реальный путь записи: EvalSymlinks резолвит симлинки.
	// При записи через симлинк-директорию (dotfiles-репо, dev-симлинк)
	// файл физически попадает в целевой каталог симлинка — сообщаем его.
	displayPath := targetPath
	if resolved, err := filepath.EvalSymlinks(targetPath); err == nil {
		displayPath = resolved
	}
	fmt.Fprintf(w, "Created: %s\n", displayPath)
	return nil
}
