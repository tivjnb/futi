package execmodule

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Module — загруженный YAML-модуль.
type Module struct {
	Name        string
	Description string
	Root        string
	Commands    map[string]*Command
}

// Command — одна команда модуля.
type Command struct {
	Description string
	Params      []Param
	Cmd         string
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
	Commands    map[string]commandYAML `yaml:"commands"`
}

type commandYAML struct {
	Description string      `yaml:"description"`
	Params      []paramYAML `yaml:"params"`
	Cmd         string      `yaml:"cmd"`
}

type paramYAML struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

// LoadModule загружает манифест модуля из YAML-файла.
// moduleRoot — директория модуля (для пакетов со скриптами).
func LoadModule(manifestPath, moduleRoot string) (*Module, error) {
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
		params := make([]Param, len(c.Params))
		for i, p := range c.Params {
			params[i] = Param{
				Name:     p.Name,
				Type:     p.Type,
				Required: p.Required,
				Default:  p.Default,
			}
		}
		commands[name] = &Command{
			Description: c.Description,
			Params:      params,
			Cmd:         c.Cmd,
		}
	}

	return &Module{
		Name:        raw.Name,
		Description: raw.Description,
		Root:        moduleRoot,
		Commands:    commands,
	}, nil
}
