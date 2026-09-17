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
// Params/Positional — схема команды по умолчанию: валидируются и
// подставляются так же, как у обычных команд (defaults, типы, min).
type Module struct {
	Name        string
	Description string
	Root        string
	Cmd         string
	Params      []Param
	Positional  Positional
	Commands    map[string]*Command
}

// Command — одна команда модуля.
// Команда может быть листом (Cmd != ""), ветвью (Commands != nil)
// или одновременно и тем, и другим — Cmd и Commands ортогональны:
// Cmd = что выполнить, Commands = куда можно спуститься дальше.
type Command struct {
	Description string
	Params      []Param
	Positional  Positional
	Cmd         string
	Commands    map[string]*Command
}

// Param — параметр команды.
type Param struct {
	Name        string
	Type        string
	Required    bool
	Default     string
	Description string
}

// Positional — требования к позиционным аргументам команды (после "--").
// В MVP поддерживается только Min — обязательный минимум позиционных аргументов.
// Ограничения (max, сравнения, остаток) и defaults — план (см. NEXT_STEPS).
type Positional struct {
	Min int
}

// moduleYAML — промежуточная структура для парсинга YAML.
type moduleYAML struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Cmd         string                 `yaml:"cmd"`
	Params      []paramYAML            `yaml:"params"`
	Positional  positionalYAML         `yaml:"positional"`
	Commands    map[string]commandYAML `yaml:"commands"`
}

type commandYAML struct {
	Description string                 `yaml:"description"`
	Params      []paramYAML            `yaml:"params"`
	Positional  positionalYAML         `yaml:"positional"`
	Cmd         string                 `yaml:"cmd"`
	Commands    map[string]commandYAML `yaml:"commands"`
}

type positionalYAML struct {
	Min int `yaml:"min"`
}

type paramYAML struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
	Description string `yaml:"description"`
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
		Params:      parseParams(raw.Params),
		Positional:  Positional{Min: raw.Positional.Min},
		Commands:    commands,
	}, nil
}

// parseParams преобразует paramsYAML в Param.
func parseParams(raw []paramYAML) []Param {
	params := make([]Param, len(raw))
	for i, p := range raw {
		params[i] = Param{
			Name:        p.Name,
			Type:        p.Type,
			Required:    p.Required,
			Default:     p.Default,
			Description: p.Description,
		}
	}
	return params
}

// parseCommand рекурсивно преобразует commandYAML в Command,
// включая вложенные commands.
func parseCommand(c commandYAML) *Command {
	cmd := &Command{
		Description: c.Description,
		Params:      parseParams(c.Params),
		Positional:  Positional{Min: c.Positional.Min},
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
