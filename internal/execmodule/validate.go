package execmodule

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"unicode"
)

// Допустимые типы параметров. Пустой тип означает string (по умолчанию).
var validParamTypes = map[string]bool{
	"":       true,
	"string": true,
	"bool":   true,
	"int":    true,
}

// Служебные ключи данных шаблона (см. Render): параметр с таким именем
// перекрывался бы ими, поэтому Validate отвергает такие имена.
var reservedParamNames = map[string]bool{
	"args":        true,
	"args_list":   true,
	"env":         true,
	"module_name": true,
	"module_root": true,
}

// paramNameRe — допустимое имя параметра: буквы, цифры, _ и -, не начинается
// с цифры или дефиса.
var paramNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// defaultPath — путь схемы default-команды модуля в сообщениях.
const defaultPath = "(default)"

// Validate проверяет схему модуля и возвращает все найденные ошибки и предупреждения.
//
// Errors (блокируют загрузку модуля в LoadModule):
//   - неизвестный ключ манифеста (с номером строки)
//   - модуль без cmd и без commands
//   - команда без cmd и без commands (бесполезная команда); cmd из одних пробелов — пустой
//   - params/positional без cmd (на любом уровне)
//   - синтаксическая ошибка шаблона в cmd, attach, detach
//   - недопустимое имя команды (пустое, с "-" в начале, с точкой, пробелами, управляющими символами)
//   - дубликаты имён params в команде
//   - type параметра ∉ {"", "string", "bool", "int"}
//   - имя параметра зарезервировано (args, args_list, env, module_name, module_root)
//   - имя параметра не соответствует ^[A-Za-z_][A-Za-z0-9_-]*$ (в т.ч. пустое)
//
// Warnings (не блокируют, показываются в futi validate):
//   - команда с commands, но без cmd (вызов без subcommand падает в runtime)
//
// Схема default-команды модуля (params/positional верхнего уровня)
// проверяется теми же правилами с путём "(default)".
// Рекурсивно обходит все команды и subcommands. Возвращает ВСЕ ошибки за один
// проход — автор модуля исправляет всё за итерацию, а не по одной ошибке.
//
// Порядок детерминирован: ошибки неизвестных ключей (порядок документа),
// attach/detach и пустой модуль, "(default)", затем команды по имени
// (на каждом уровне родитель раньше детей).
func (m *Module) Validate() (errors []error, warnings []string) {
	errors = append(errors, m.schemaErrs...)

	validateHook("attach", m.Attach, &errors)
	validateHook("detach", m.Detach, &errors)

	if strings.TrimSpace(m.Cmd) == "" && len(m.Commands) == 0 {
		errors = append(errors, fmt.Errorf("module has no cmd and no commands"))
	}

	// Схема default-команды проверяется, когда у модуля есть cmd или params/positional.
	// Commands передаём, чтобы пустой cmd не давал «must have cmd or commands»
	// (это отдельное правило модуля выше).
	hasDefaultSchema := strings.TrimSpace(m.Cmd) != "" || len(m.Params) > 0 || m.Positional.Min != 0
	if hasDefaultSchema {
		checkCommand(defaultPath, &Command{
			Cmd:        m.Cmd,
			Params:     m.Params,
			Positional: m.Positional,
			Commands:   m.Commands,
		}, &errors, &warnings)
	}
	validateSubcommands("", m.Commands, &errors, &warnings)
	return errors, warnings
}

// validateHook проверяет синтаксис шаблона хука (attach/detach).
func validateHook(name, tmpl string, errors *[]error) {
	if _, err := template.New(name).Funcs(TemplateFuncs()).Parse(tmpl); err != nil {
		*errors = append(*errors, fmt.Errorf("%s: invalid template: %w", name, err))
	}
}

// validateSubcommands обходит команды в порядке отсортированных имён:
// проверяет имя, саму команду и рекурсивно её subcommands.
// parent — полный путь родителя ("" на верхнем уровне).
func validateSubcommands(parent string, cmds map[string]*Command, errors *[]error, warnings *[]string) {
	names := make([]string, 0, len(cmds))
	for name := range cmds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := name
		if parent != "" {
			path = parent + "." + name
		}
		if !validCommandName(name) {
			*errors = append(*errors, fmt.Errorf("command %q: invalid command name (must be non-empty, not start with \"-\", no dots, whitespace or control characters)", path))
		}
		validateCommand(path, cmds[name], errors, warnings)
	}
}

// validCommandName — имя команды допустимо: непустое, не начинается с "-",
// без точки (разделитель пути), пробельных и управляющих символов.
func validCommandName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	for _, r := range name {
		if r == '.' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// validateCommand проверяет команду и рекурсивно её subcommands.
// path — имя команды (для сообщений об ошибках).
func validateCommand(path string, cmd *Command, errors *[]error, warnings *[]string) {
	checkCommand(path, cmd, errors, warnings)
	validateSubcommands(path, cmd.Commands, errors, warnings)
}

// checkCommand проверяет одну команду без спуска в subcommands.
// Сюда добавляются правила уровня команды.
func checkCommand(path string, cmd *Command, errors *[]error, warnings *[]string) {
	// Проверка: команда должна иметь cmd или commands (или оба).
	hasCmd := strings.TrimSpace(cmd.Cmd) != ""
	hasCommands := len(cmd.Commands) > 0

	if !hasCmd && !hasCommands {
		*errors = append(*errors, fmt.Errorf("command %q: must have cmd or commands", path))
	}

	// Предупреждение: команда с commands, но без cmd — вызов без subcommand падает.
	// Для (default) не выдаётся: модуль без default-cmd — обычный случай.
	if hasCommands && !hasCmd && path != defaultPath {
		*warnings = append(*warnings, fmt.Sprintf("command %q: has subcommands but no cmd (calling without subcommand will fail)", path))
	}

	// params/positional не наследуются подкомандами: без cmd они бессмысленны.
	if !hasCmd && (len(cmd.Params) > 0 || cmd.Positional.Min != 0) {
		*errors = append(*errors, fmt.Errorf("command %q: params/positional require cmd", path))
	}

	if hasCmd {
		if _, err := template.New(path).Funcs(TemplateFuncs()).Parse(cmd.Cmd); err != nil {
			*errors = append(*errors, fmt.Errorf("command %q: invalid cmd template: %w", path, err))
		}
	}

	// Проверка positional: min не может быть отрицательным.
	if cmd.Positional.Min < 0 {
		*errors = append(*errors, fmt.Errorf("command %q: positional min must be >= 0, got %d", path, cmd.Positional.Min))
	}

	// Проверка params: дубликаты имён и невалидные типы.
	seen := make(map[string]bool)
	for _, p := range cmd.Params {
		if seen[p.Name] {
			*errors = append(*errors, fmt.Errorf("command %q: duplicate param %q", path, p.Name))
		}
		seen[p.Name] = true

		// Зарезервированные имена перекрылись бы служебными ключами шаблона.
		if reservedParamNames[p.Name] {
			*errors = append(*errors, fmt.Errorf("command %q: param %q is reserved (args, args_list, env, module_name, module_root)", path, p.Name))
		} else if !paramNameRe.MatchString(p.Name) {
			*errors = append(*errors, fmt.Errorf("command %q: invalid param name %q (allowed: letters, digits, _ and -, not starting with a digit or -)", path, p.Name))
		}

		if !validParamTypes[p.Type] {
			*errors = append(*errors, fmt.Errorf("command %q: param %q has invalid type %q (valid: string, bool, int)", path, p.Name, p.Type))
		}
	}
}

// FormatValidateReport форматирует ошибки и предупреждения в читаемый отчёт.
// Используется командой `futi validate` для вывода результатов.
// Возвращает строку и флаг hasErrors (true если есть errors).
func FormatValidateReport(errors []error, warnings []string) (string, bool) {
	var buf strings.Builder

	hasErrors := len(errors) > 0

	for _, e := range errors {
		fmt.Fprintf(&buf, "ERROR: %s\n", e)
	}

	for _, w := range warnings {
		fmt.Fprintf(&buf, "WARNING: %s\n", w)
	}

	if !hasErrors && len(warnings) == 0 {
		buf.WriteString("OK\n")
	}

	return buf.String(), hasErrors
}
