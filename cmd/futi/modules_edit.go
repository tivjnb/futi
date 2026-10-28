package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tivjnb/futi/internal/execmodule"

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

// manifestRoot возвращает корневой mapping документа. Пустой документ (нет нод,
// только комментарии или явный null: `---`, `~`, `null`): при create создаётся корневой mapping, иначе
// возвращается nil (findMappingValue корректно работает с nil). Корень не
// mapping — ошибка.
func manifestRoot(doc *yaml.Node, path string, create bool) (*yaml.Node, error) {
	if len(doc.Content) == 0 || isNullNode(doc.Content[0]) {
		if !create {
			return nil, nil
		}
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{root}
		return root, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("manifest root is not a mapping in %s", path)
	}
	return root, nil
}

// isNullNode сообщает, что нода — скаляр null.
func isNullNode(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

// addCommandToManifest добавляет команду cmdName в commands: манифеста path.
// cmdStr — тело команды; desc — описание (опционально).
// Ошибка, если команда уже существует. Результат проверяется до записи
// (writeValidated); при невалидном результате оригинал не меняется.
func addCommandToManifest(path, cmdName, cmdStr, desc string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	root, err := manifestRoot(&doc, path, true)
	if err != nil {
		return err
	}

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

	return writeValidated(path, &doc)
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
	root, err := manifestRoot(&doc, path, false)
	if err != nil {
		return err
	}

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

	return writeValidated(path, &doc)
}

// writeValidated сериализует doc и заменяет манифест path атомарно: данные
// пишутся во временный файл в том же каталоге и только затем переименовываются
// поверх оригинала. Результат проверяется в памяти (LoadManifest) до создания
// temp-файла. Оригинал при невалидном результате или сбое записи не
// трогается; права файла сохраняются.
//
// Если path — симлинк, правится его цель (EvalSymlinks): модуль = код, и
// пользователь сам редактирует свой модуль; rename поверх симлинка заменил бы
// сам симлинк обычным файлом. В сообщении об ошибке — путь path в том виде,
// в каком его знает пользователь (для симлинка — сам симлинк), а moduleRoot
// берётся от настоящего каталога манифеста.
func writeValidated(path string, doc *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return fmt.Errorf("stat manifest: %w", err)
	}
	dir := filepath.Dir(real)

	if _, err := execmodule.LoadManifest(buf.Bytes(), path, dir); err != nil {
		return fmt.Errorf("edited manifest is invalid, original kept: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(real)+"-*")
	if err != nil {
		return fmt.Errorf("create temp manifest: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op после успешного rename

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	// Сохраняются только права Perm(): владелец, группа и биты сверх Perm()
	// (setuid и т.п.) при замене через temp+rename не переносятся.
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	if err := os.Rename(tmpPath, real); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	// fsync каталога фиксирует rename; best-effort: на Windows каталог для
	// Sync не открыть, ошибки игнорируем (как в metayaml.Write).
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// AddCommandOptions are the arguments of Manager.AddCommand. An empty Cmd
// defaults to "echo <Command>".
type AddCommandOptions struct {
	Module, Command, Desc, Cmd string
}

// RemoveCommandOptions are the arguments of Manager.RemoveCommand.
type RemoveCommandOptions struct {
	Module, Command string
}

// AddCommand adds a command to the module's config.yaml.
func (m *Manager) AddCommand(opts AddCommandOptions) error {
	if err := validateResolveName(opts.Module); err != nil {
		return err
	}
	source, err := m.rt.Source()
	if err != nil {
		return err
	}
	manifestPath, _, err := source.Resolve(opts.Module)
	if err != nil {
		return fmt.Errorf("modules add_command: %w", err)
	}

	cmdStr := opts.Cmd
	if cmdStr == "" {
		cmdStr = "echo " + opts.Command
	}
	if err := addCommandToManifest(manifestPath, opts.Command, cmdStr, opts.Desc); err != nil {
		return fmt.Errorf("modules add_command: %w", err)
	}
	if err := m.rt.Cache().Invalidate(); err != nil {
		fmt.Fprintf(m.s.Err, "cache: %v (continuing)\n", err)
	}
	fmt.Fprintf(m.s.Out, "Added command %q to %s\n", opts.Command, manifestPath)
	return nil
}

// RemoveCommand removes a command from the module's config.yaml.
func (m *Manager) RemoveCommand(opts RemoveCommandOptions) error {
	if err := validateResolveName(opts.Module); err != nil {
		return err
	}
	source, err := m.rt.Source()
	if err != nil {
		return err
	}
	manifestPath, _, err := source.Resolve(opts.Module)
	if err != nil {
		return fmt.Errorf("modules remove_command: %w", err)
	}
	if err := removeCommandFromManifest(manifestPath, opts.Command); err != nil {
		return fmt.Errorf("modules remove_command: %w", err)
	}
	if err := m.rt.Cache().Invalidate(); err != nil {
		fmt.Fprintf(m.s.Err, "cache: %v (continuing)\n", err)
	}
	fmt.Fprintf(m.s.Out, "Removed command %q from %s\n", opts.Command, manifestPath)
	return nil
}
