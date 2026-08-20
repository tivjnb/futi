package execmodule

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Module — загруженный YAML-модуль.
// Cmd — опциональная команда по умолчанию: вызывается, если не указана
// конкретная команда. Позволяет использовать модуль как одну команду:
// `futi echo_mod -- hello world` выполнит Cmd с args="hello world".
type Module struct {
	Name        string
	Description string
	Root        string
	Cmd         string
	Commands    map[string]*Command
}

// Command — одна команда модуля.
// Команда может быть листом (Cmd != ""), ветвью (Commands != nil)
// или одновременно и тем, и другим — Cmd и Commands ортогональны:
// Cmd = что выполнить, Commands = куда можно спуститься дальше.
type Command struct {
	Description string
	Params      []Param
	Cmd         string
	Commands    map[string]*Command
}

// Param — параметр команды.
type Param struct {
	Name     string
	Type     string
	Required bool
	Default  string
}

// moduleYAML — промежуточная структура для парсинга YAML.
type moduleYAML struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Cmd         string                 `yaml:"cmd"`
	Commands    map[string]commandYAML `yaml:"commands"`
}

type commandYAML struct {
	Description string                 `yaml:"description"`
	Params      []paramYAML            `yaml:"params"`
	Cmd         string                 `yaml:"cmd"`
	Commands    map[string]commandYAML `yaml:"commands"`
}

type paramYAML struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

// LoadModule загружает манифест модуля из YAML-файла и валидирует схему.
// moduleRoot — директория модуля (для пакетов со скриптами).
// Возвращает ошибку при невалидной схеме (первая ошибка из Validate).
// Для получения полного отчёта всех ошибок используйте ParseModule + Validate.
func LoadModule(manifestPath, moduleRoot string) (*Module, error) {
	mod, err := ParseModule(manifestPath, moduleRoot)
	if err != nil {
		return nil, err
	}

	errors, _ := mod.Validate()
	if len(errors) > 0 {
		return nil, fmt.Errorf("execmodule: invalid module %q: %w", manifestPath, errors[0])
	}

	return mod, nil
}

// ParseModule загружает манифест модуля из YAML-файла без валидации схемы.
// Используется `futi validate` для получения полного отчёта всех ошибок
// через последующий вызов Module.Validate().
// moduleRoot — директория модуля (для пакетов со скриптами).
func ParseModule(manifestPath, moduleRoot string) (*Module, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("execmodule: read manifest %q: %w", manifestPath, err)
	}

	var raw moduleYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("execmodule: parse manifest %q: %w", manifestPath, err)
	}

	commands := make(map[string]*Command, len(raw.Commands))
	for name, c := range raw.Commands {
		commands[name] = parseCommand(c)
	}

	return &Module{
		Name:        raw.Name,
		Description: raw.Description,
		Root:        moduleRoot,
		Cmd:         raw.Cmd,
		Commands:    commands,
	}, nil
}

// parseCommand рекурсивно преобразует commandYAML в Command,
// включая вложенные commands.
func parseCommand(c commandYAML) *Command {
	params := make([]Param, len(c.Params))
	for i, p := range c.Params {
		params[i] = Param{
			Name:     p.Name,
			Type:     p.Type,
			Required: p.Required,
			Default:  p.Default,
		}
	}

	cmd := &Command{
		Description: c.Description,
		Params:      params,
		Cmd:         c.Cmd,
	}

	if len(c.Commands) > 0 {
		cmd.Commands = make(map[string]*Command, len(c.Commands))
		for name, sub := range c.Commands {
			cmd.Commands[name] = parseCommand(sub)
		}
	}

	return cmd
}
