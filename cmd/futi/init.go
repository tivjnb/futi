package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"futi/internal/config"
)

// initConfig — параметры команды futi init.
type initConfig struct {
	path        string   // путь модуля (может содержать / для namespace)
	global      bool     // создавать в ~/.config/futi/modules/ вместо CWD
	description string   // описание модуля
	defaultCmd  string   // cmd на уровне модуля
	commands    []string // имена команд для шаблона
}

// parseInitArgs разбирает аргументы команды futi init.
// Позиционный аргумент — path (обязательный). Флаги:
// --global, --desc <value>, --default-cmd <value>, --command <name> (repeatable).
func parseInitArgs(args []string) (*initConfig, error) {
	cfg := &initConfig{}
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--global":
			cfg.global = true
		case arg == "--desc":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--desc requires a value")
			}
			cfg.description = args[i+1]
			i++
		case strings.HasPrefix(arg, "--desc="):
			cfg.description = strings.TrimPrefix(arg, "--desc=")
		case arg == "--default-cmd":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--default-cmd requires a value")
			}
			cfg.defaultCmd = args[i+1]
			i++
		case strings.HasPrefix(arg, "--default-cmd="):
			cfg.defaultCmd = strings.TrimPrefix(arg, "--default-cmd=")
		case arg == "--command":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--command requires a value")
			}
			cfg.commands = append(cfg.commands, args[i+1])
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
		return nil, fmt.Errorf("usage: futi init <name> [flags]")
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one path argument, got %d", len(positional))
	}
	cfg.path = positional[0]

	if cfg.description == "" {
		cfg.description = "My tool"
	}

	return cfg, nil
}

// targetFilePath возвращает путь к файлу модуля.
// Локально: ./.futi/<path>.yaml (доступен через auto-discovery).
// Глобально: ~/.config/futi/modules/<path>.yaml.
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
	return filepath.Join(base, cfg.path+".yaml"), nil
}

// generateModuleYAML генерирует YAML-шаблон модуля.
// name — имя модуля (из последнего сегмента path).
// description — описание (без кавычек для простых значений, чище для редактирования).
// defaultCmd — cmd на уровне модуля (пусто = не добавлять).
// commands — имена команд (если пусто — одна команда hello по умолчанию).
func generateModuleYAML(name, description, defaultCmd string, commands []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: %s\n", description)
	if defaultCmd != "" {
		fmt.Fprintf(&b, "cmd: %s\n", quoteYAMLValue(defaultCmd))
	}
	b.WriteString("commands:\n")
	if len(commands) > 0 {
		for _, cmd := range commands {
			fmt.Fprintf(&b, "  %s:\n    cmd: %s\n", cmd, quoteYAMLValue("echo "+cmd))
		}
	} else {
		b.WriteString("  hello:\n    cmd: \"echo hello\"\n")
	}
	return b.String()
}

// quoteYAMLValue оборачивает значение в двойные кавычки для YAML.
// Использует Go-экранирование (%q), которое совместимо с YAML double-quoted style.
func quoteYAMLValue(s string) string {
	return fmt.Sprintf("%q", s)
}

// runInit создаёт новый модуль с шаблоном YAML.
// Парсит path + флаги, генерирует шаблон, пишет файл.
// Возвращает ошибку при существующем файле (без перезаписи).
func runInit(w io.Writer, args []string) error {
	cfg, err := parseInitArgs(args)
	if err != nil {
		return err
	}

	targetPath, err := targetFilePath(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(targetPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("%s already exists", targetPath)
	}

	name := filepath.Base(cfg.path)
	yaml := generateModuleYAML(name, cfg.description, cfg.defaultCmd, cfg.commands)

	if err := os.WriteFile(targetPath, []byte(yaml), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	fmt.Fprintf(w, "Created: %s\n", targetPath)
	return nil
}
