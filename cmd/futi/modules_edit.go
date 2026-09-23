package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"futi/internal/discovery"
	"futi/internal/execmodule"

	"gopkg.in/yaml.v3"
)

// findMappingValue находит значение ключа key в mapping-ноде (nil, если нет).
func findMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// ensureMapping находит или создаёт ключ key в mapping-ноде root.
// Возвращает ноду-значение (новую пустую mapping при создании).
func ensureMapping(root *yaml.Node, key string) *yaml.Node {
	if existing := findMappingValue(root, key); existing != nil {
		return existing
	}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, valNode)
	return valNode
}

// scalarStr строит scalar-ноду со строковым значением.
func scalarStr(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// addCommandToManifest добавляет команду cmdName в commands: манифеста path.
// cmdStr — тело команды; desc — описание (опционально).
// Ошибка, если команда уже существует. После записи манифест валидируется;
// при провале восстанавливается оригинал.
func addCommandToManifest(path, cmdName, cmdStr, desc string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	root := doc.Content[0]

	commands := ensureMapping(root, "commands")
	if findMappingValue(commands, cmdName) != nil {
		return fmt.Errorf("command %q already exists in %s", cmdName, path)
	}

	cmdNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	cmdNode.Content = append(cmdNode.Content, scalarStr("cmd"), scalarStr(cmdStr))
	if desc != "" {
		cmdNode.Content = append(cmdNode.Content, scalarStr("description"), scalarStr(desc))
	}
	commands.Content = append(commands.Content, scalarStr(cmdName), cmdNode)

	return writeValidated(path, data, &doc)
}

// removeCommandFromManifest удаляет команду cmdName из commands: манифеста.
// Ошибка, если команды нет.
func removeCommandFromManifest(path, cmdName string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	root := doc.Content[0]

	commands := findMappingValue(root, "commands")
	if commands == nil {
		return fmt.Errorf("command %q not found in %s", cmdName, path)
	}

	var kept []*yaml.Node
	removed := false
	for i := 0; i+1 < len(commands.Content); i += 2 {
		if commands.Content[i].Value == cmdName {
			removed = true
			continue // пропускаем пару ключ-значение
		}
		kept = append(kept, commands.Content[i], commands.Content[i+1])
	}
	if !removed {
		return fmt.Errorf("command %q not found in %s", cmdName, path)
	}
	commands.Content = kept

	return writeValidated(path, data, &doc)
}

// writeValidated сериализует doc в path и проверяет, что результат — валидный
// манифест (LoadModule). При невалидности восстанавливает оригинал (orig).
func writeValidated(path string, orig []byte, doc *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	if _, err := execmodule.LoadModule(path, filepath.Dir(path)); err != nil {
		if werr := os.WriteFile(path, orig, 0o644); werr != nil {
			return fmt.Errorf("edited manifest is invalid (%v); restore failed: %v", err, werr)
		}
		return fmt.Errorf("edited manifest is invalid, original restored: %w", err)
	}
	return nil
}

// addCommandArgs — аргументы `futi modules add_command`.
type addCommandArgs struct {
	module  string
	command string
	desc    string
	cmd     string
}

// parseAddCommandArgs разбирает аргументы: позиционные module, command;
// флаги --desc <text>, --cmd <shell> (паттерн requireValue).
func parseAddCommandArgs(args []string) (*addCommandArgs, error) {
	cfg := &addCommandArgs{}
	var positional []string

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
		case arg == "--desc":
			val, err := requireValue("--desc", i)
			if err != nil {
				return nil, err
			}
			cfg.desc = val
			i++
		case strings.HasPrefix(arg, "--desc="):
			cfg.desc = strings.TrimPrefix(arg, "--desc=")
		case arg == "--cmd":
			val, err := requireValue("--cmd", i)
			if err != nil {
				return nil, err
			}
			cfg.cmd = val
			i++
		case strings.HasPrefix(arg, "--cmd="):
			cfg.cmd = strings.TrimPrefix(arg, "--cmd=")
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) != 2 {
		return nil, fmt.Errorf("usage: futi modules add_command <module> <command> [--desc <text>] [--cmd <shell>]")
	}
	cfg.module, cfg.command = positional[0], positional[1]
	return cfg, nil
}

// runModulesAddCommand добавляет команду в config.yaml модуля.
func runModulesAddCommand(w io.Writer, source discovery.Source, args []string) error {
	cfg, err := parseAddCommandArgs(args)
	if err != nil {
		return err
	}
	manifestPath, _, err := source.Resolve(cfg.module)
	if err != nil {
		return fmt.Errorf("modules add_command: %w", err)
	}

	cmdStr := cfg.cmd
	if cmdStr == "" {
		cmdStr = "echo " + cfg.command
	}
	if err := addCommandToManifest(manifestPath, cfg.command, cmdStr, cfg.desc); err != nil {
		return fmt.Errorf("modules add_command: %w", err)
	}
	fmt.Fprintf(w, "Added command %q to %s\n", cfg.command, manifestPath)
	return nil
}

// runModulesRemoveCommand удаляет команду из config.yaml модуля.
func runModulesRemoveCommand(w io.Writer, source discovery.Source, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: futi modules remove_command <module> <command>")
	}
	module, command := args[0], args[1]

	manifestPath, _, err := source.Resolve(module)
	if err != nil {
		return fmt.Errorf("modules remove_command: %w", err)
	}
	if err := removeCommandFromManifest(manifestPath, command); err != nil {
		return fmt.Errorf("modules remove_command: %w", err)
	}
	fmt.Fprintf(w, "Removed command %q from %s\n", command, manifestPath)
	return nil
}
