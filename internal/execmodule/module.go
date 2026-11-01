package execmodule

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/inputs"
	"github.com/tivjnb/futi/internal/yamlstrict"
	"gopkg.in/yaml.v3"
)

// CmdSpec is the cmd of a node in its source form: a string (Text) or a list
// of strings (List, one argv element per item).
type CmdSpec struct {
	Text   string
	List   []string
	IsList bool
}

// UnmarshalYAML reads a scalar or a list of scalars.
func (c *CmdSpec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.ScalarNode:
		*c = CmdSpec{Text: n.Value}
		return nil
	case yaml.SequenceNode:
		list := make([]string, 0, len(n.Content))
		for _, item := range n.Content {
			if item.Kind == yaml.AliasNode && item.Alias != nil {
				item = item.Alias
			}
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: cmd must be a string or a list of strings", item.Line)
			}
			list = append(list, item.Value)
		}
		*c = CmdSpec{List: list, IsList: true}
		return nil
	}
	return fmt.Errorf("line %d: cmd must be a string or a list of strings", n.Line)
}

// IsZero reports a cmd without any content: absent, blank or an empty list.
func (c CmdSpec) IsZero() bool {
	if c.IsList {
		return len(c.List) == 0
	}
	return strings.TrimSpace(c.Text) == ""
}

// Template parses the cmd in the form it was written in.
func (c CmdSpec) Template() (*argvtmpl.Template, error) {
	if c.IsList {
		return argvtmpl.ParseList(c.List)
	}
	return argvtmpl.Parse(c.Text)
}

// Display returns the cmd for messages: a string as is, a list as
// [a, "b c"] (items with whitespace, commas, quotes or brackets are quoted).
func (c CmdSpec) Display() string {
	if !c.IsList {
		return c.Text
	}
	items := make([]string, len(c.List))
	for i, it := range c.List {
		items[i] = it
		if it == "" || strings.ContainsAny(it, " \t\n,\"'[]") {
			items[i] = strconv.Quote(it)
		}
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// Command is a node of the module: the module itself or one command. A node
// may have a cmd (what to run), args and flags (its inputs) and commands
// (where to descend) at the same time.
type Command struct {
	Description string
	Cmd         CmdSpec
	Args        []inputs.Spec
	Flags       []inputs.Spec // Spec.Flag is true for every element
	Commands    map[string]*Command
}

// Module is a loaded YAML module. Its own node (the default command of the
// module) is the embedded Command.
type Module struct {
	Name   string
	Root   string
	Attach string // setup hook: futi modules install/update
	Detach string // teardown hook: futi modules uninstall
	Command

	// schemaErrs holds the manifest schema errors (with line numbers) found
	// by ParseManifest; Validate returns them first.
	schemaErrs []error
}

// moduleYAML is the YAML form of a module.
type moduleYAML struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Cmd         CmdSpec                `yaml:"cmd"`
	Attach      string                 `yaml:"attach"`
	Detach      string                 `yaml:"detach"`
	Args        []inputYAML            `yaml:"args"`
	Flags       []inputYAML            `yaml:"flags"`
	Commands    map[string]commandYAML `yaml:"commands"`

	// Keys of the old format: accepted here so that the unknown-key check
	// skips them; ParseManifest reports them with their own message.
	OldParams     any `yaml:"params"`
	OldPositional any `yaml:"positional"`
}

type commandYAML struct {
	Description string                 `yaml:"description"`
	Cmd         CmdSpec                `yaml:"cmd"`
	Args        []inputYAML            `yaml:"args"`
	Flags       []inputYAML            `yaml:"flags"`
	Commands    map[string]commandYAML `yaml:"commands"`

	OldParams     any `yaml:"params"`
	OldPositional any `yaml:"positional"`
}

type inputYAML struct {
	Name        string  `yaml:"name"`
	Type        string  `yaml:"type"`
	Required    bool    `yaml:"required"`
	Default     *string `yaml:"default"`
	Description string  `yaml:"description"`
	Short       string  `yaml:"short"`
	Variadic    bool    `yaml:"variadic"`
	Rest        bool    `yaml:"rest"`
	AllowDash   bool    `yaml:"allow_dash"`
}

// LoadModule загружает манифест модуля из YAML-файла и валидирует схему.
// moduleRoot — директория модуля (для пакетов со скриптами).
// Возвращает ошибку при невалидной схеме (первая ошибка из Validate).
// Для получения полного отчёта всех ошибок используйте ParseModule + Validate.
func LoadModule(manifestPath, moduleRoot string) (*Module, error) {
	data, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	return LoadManifest(data, manifestPath, moduleRoot)
}

// LoadManifest — LoadModule для манифеста, уже лежащего в памяти: проверяет
// размер (MaxManifestSize), разбирает и возвращает первую ошибку Validate.
// manifestPath используется только в сообщениях об ошибках (для ещё не
// записанного файла — будущий путь).
func LoadManifest(data []byte, manifestPath, moduleRoot string) (*Module, error) {
	if len(data) > MaxManifestSize {
		return nil, fmt.Errorf("execmodule: manifest %q: size %d bytes exceeds limit %d", manifestPath, len(data), MaxManifestSize)
	}
	mod, err := ParseManifest(data, manifestPath, moduleRoot)
	if err != nil {
		return nil, err
	}

	errors, _ := mod.Validate()
	if len(errors) > 0 {
		return nil, fmt.Errorf("execmodule: invalid module %q: %w", manifestPath, errors[0])
	}

	return mod, nil
}

// MaxManifestSize — предельный размер манифеста модуля (1 МиБ). Общий для
// всех, кто читает манифест (в т.ч. хеширование в modulecache).
const MaxManifestSize = 1 << 20

// readManifest читает манифест, принимая только обычный файл не больше
// MaxManifestSize: FIFO/устройства блокируют чтение, огромный файл — память.
func readManifest(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("execmodule: read manifest %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("execmodule: manifest %q is not a regular file", path)
	}
	if info.Size() > MaxManifestSize {
		return nil, fmt.Errorf("execmodule: manifest %q: size %d bytes exceeds limit %d", path, info.Size(), MaxManifestSize)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("execmodule: read manifest %q: %w", path, err)
	}
	defer f.Close()
	// limit+1 байт: файл мог вырасти после Stat, усечение молча недопустимо.
	data, err := io.ReadAll(io.LimitReader(f, MaxManifestSize+1))
	if err != nil {
		return nil, fmt.Errorf("execmodule: read manifest %q: %w", path, err)
	}
	if len(data) > MaxManifestSize {
		return nil, fmt.Errorf("execmodule: manifest %q: size exceeds limit of %d bytes", path, MaxManifestSize)
	}
	return data, nil
}

// ParseModule загружает манифест модуля из YAML-файла без валидации схемы.
// Используется `futi validate` для получения полного отчёта всех ошибок
// через последующий вызов Module.Validate().
// moduleRoot — директория модуля (для пакетов со скриптами).
func ParseModule(manifestPath, moduleRoot string) (*Module, error) {
	data, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	return ParseManifest(data, manifestPath, moduleRoot)
}

// ParseManifest разбирает манифест из байтов без Validate. manifestPath
// используется только в сообщениях об ошибках (для ещё не записанного файла —
// будущий путь).
func ParseManifest(data []byte, manifestPath, moduleRoot string) (*Module, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("execmodule: parse manifest %q: %w", manifestPath, err)
	}
	schemaErrs := yamlstrict.UnknownKeys(&node, reflect.TypeOf(moduleYAML{}))
	schemaErrs = append(schemaErrs, oldFormatErrors(&node)...)
	// Пустой документ (Kind == 0) не декодируем: получится пустой модуль,
	// его отвергнет Validate.
	var raw moduleYAML
	if node.Kind != 0 {
		if err := node.Decode(&raw); err != nil {
			return nil, fmt.Errorf("execmodule: parse manifest %q: %w", manifestPath, err)
		}
	}

	return &Module{
		Name:   raw.Name,
		Root:   moduleRoot,
		Attach: raw.Attach,
		Detach: raw.Detach,
		Command: *parseCommand(commandYAML{
			Description: raw.Description,
			Cmd:         raw.Cmd,
			Args:        raw.Args,
			Flags:       raw.Flags,
			Commands:    raw.Commands,
		}),
		schemaErrs: schemaErrs,
	}, nil
}

// oldFormatErrors reports the keys params and positional of the old module
// format on the module and on every command, in document order.
func oldFormatErrors(root *yaml.Node) []error {
	var errs []error
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for n != nil && (n.Kind == yaml.DocumentNode || n.Kind == yaml.AliasNode) {
			if n.Kind == yaml.AliasNode {
				n = n.Alias
			} else if len(n.Content) > 0 {
				n = n.Content[0]
			} else {
				return
			}
		}
		if n == nil || n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i], n.Content[i+1]
			switch key.Value {
			case "params", "positional":
				errs = append(errs, fmt.Errorf("line %d: %q was replaced by \"args\"/\"flags\" (see README)", key.Line, key.Value))
			case "commands":
				if val.Kind == yaml.AliasNode && val.Alias != nil {
					val = val.Alias
				}
				if val.Kind == yaml.MappingNode {
					for j := 1; j < len(val.Content); j += 2 {
						walk(val.Content[j])
					}
				}
			}
		}
	}
	walk(root)
	return errs
}

// parseInputs converts the YAML form of args or flags to specs.
func parseInputs(raw []inputYAML, flag bool) []inputs.Spec {
	if len(raw) == 0 {
		return nil
	}
	specs := make([]inputs.Spec, len(raw))
	for i, in := range raw {
		specs[i] = inputs.Spec{
			Name:        in.Name,
			Type:        in.Type,
			Description: in.Description,
			Short:       in.Short,
			Required:    in.Required,
			Variadic:    in.Variadic,
			Rest:        in.Rest,
			AllowDash:   in.AllowDash,
			Default:     in.Default,
			Flag:        flag,
		}
	}
	return specs
}

// parseCommand рекурсивно преобразует commandYAML в Command,
// включая вложенные commands.
func parseCommand(c commandYAML) *Command {
	cmd := &Command{
		Description: c.Description,
		Cmd:         c.Cmd,
		Args:        parseInputs(c.Args, false),
		Flags:       parseInputs(c.Flags, true),
	}

	if len(c.Commands) > 0 {
		cmd.Commands = make(map[string]*Command, len(c.Commands))
		for name, sub := range c.Commands {
			cmd.Commands[name] = parseCommand(sub)
		}
	}

	return cmd
}
