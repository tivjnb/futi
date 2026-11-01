package execmodule

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/inputs"
)

// defaultPath is the path of the module's own command in messages.
const defaultPath = "(default)"

// Validate проверяет схему модуля и возвращает все найденные ошибки и предупреждения.
//
// Errors (блокируют загрузку модуля в LoadModule):
//   - неизвестный ключ манифеста (с номером строки); ключи params и positional
//     старого формата
//   - модуль без cmd и без commands
//   - команда без cmd и без commands (бесполезная команда); cmd из одних пробелов — пустой
//   - args/flags без cmd (на любом уровне)
//   - ошибки схемы входов (inputs.Validate) с префиксом command "<path>"
//   - ошибки шаблона cmd: синтаксис, запрещённые конструкции, ссылки на
//     необъявленные входы, неупомянутые входы (argvtmpl.Check)
//   - ошибки шаблона в attach, detach: синтаксис, любой вход ({{.x}}); доступны
//     только env, module_root, module_name
//   - недопустимое имя команды (пустое, с "-" в начале, с точкой, пробелами, управляющими символами)
//
// Warnings (не блокируют, показываются в futi validate):
//   - команда с commands, но без cmd (вызов без subcommand падает в runtime)
//   - элемент хука attach/detach — оператор shell (&&, ||, |, >, <, >>): хуки
//     идут без shell, он попадёт программе аргументом
//   - подкоманда перекрывает значения позиционного string-входа (ShadowWarnings)
//
// Схема default-команды модуля (args/flags верхнего уровня)
// проверяется теми же правилами с путём "(default)".
// Рекурсивно обходит все команды и subcommands. Возвращает ВСЕ ошибки за один
// проход — автор модуля исправляет всё за итерацию, а не по одной ошибке.
//
// Порядок детерминирован: ошибки неизвестных ключей (порядок документа),
// attach/detach и пустой модуль, "(default)", затем команды по имени
// (на каждом уровне родитель раньше детей).
func (m *Module) Validate() (errors []error, warnings []string) {
	errors = append(errors, m.schemaErrs...)

	validateHook("attach", m.Attach, &errors, &warnings)
	validateHook("detach", m.Detach, &errors, &warnings)

	if m.Cmd.IsZero() && len(m.Commands) == 0 {
		errors = append(errors, fmt.Errorf("module has no cmd and no commands"))
	}

	// Схема default-команды проверяется, когда у модуля есть cmd или args/flags.
	// Отсутствие cmd и commands у модуля сообщает отдельное правило выше.
	hasDefaultSchema := !m.Cmd.IsZero() || len(m.Args) > 0 || len(m.Flags) > 0
	if hasDefaultSchema {
		checkCommand(defaultPath, true, &m.Command, &errors, &warnings)
	}
	validateSubcommands("", m.Commands, &errors, &warnings)
	return errors, warnings
}

// shellOperators are the words that mean something to a shell but are plain
// arguments in a hook, which runs without one.
var shellOperators = []string{"&&", "||", "|", ">", "<", ">>"}

// validateHook checks the template of a hook (attach/detach) the way cmd is
// checked, but with no inputs: only env, module_root and module_name are
// available. A literal shell operator element is a warning.
func validateHook(name string, hook CmdSpec, errors *[]error, warnings *[]string) {
	if hook.IsZero() {
		return
	}
	tmpl, err := hook.Template()
	if err != nil {
		*errors = append(*errors, fmt.Errorf("%s: %w", name, err))
		return
	}
	for _, err := range tmpl.Check(nil) {
		*errors = append(*errors, fmt.Errorf("%s: %w", name, err))
	}
	var seen []string
	for _, lit := range tmpl.Literals() {
		if slices.Contains(shellOperators, lit) && !slices.Contains(seen, lit) {
			seen = append(seen, lit)
			*warnings = append(*warnings, fmt.Sprintf("%s: %q is passed to the program as an argument (hooks run without a shell); use [sh, -c, \"…\"]", name, lit))
		}
	}
}

// validateSubcommands обходит команды в порядке отсортированных имён:
// проверяет имя, саму команду и рекурсивно её subcommands.
// parent — полный путь родителя ("" на верхнем уровне).
func validateSubcommands(parent string, cmds map[string]*Command, errors *[]error, warnings *[]string) {
	for _, name := range sortedNames(cmds) {
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
	checkCommand(path, false, cmd, errors, warnings)
	validateSubcommands(path, cmd.Commands, errors, warnings)
}

// checkCommand проверяет одну команду без спуска в subcommands.
// Сюда добавляются правила уровня команды.
func checkCommand(path string, isDefault bool, cmd *Command, errors *[]error, warnings *[]string) {
	// Проверка: команда должна иметь cmd или commands (или оба).
	hasCmd := !cmd.Cmd.IsZero()
	hasCommands := len(cmd.Commands) > 0

	// Для (default) это правило не применяется: его покрывает ошибка модуля.
	if !hasCmd && !hasCommands && !isDefault {
		*errors = append(*errors, fmt.Errorf("command %q: must have cmd or commands", path))
	}

	// Предупреждение: команда с commands, но без cmd — вызов без subcommand падает.
	// Для (default) не выдаётся: модуль без default-cmd — обычный случай.
	if hasCommands && !hasCmd && !isDefault {
		*warnings = append(*warnings, fmt.Sprintf("command %q: has subcommands but no cmd (calling without subcommand will fail)", path))
	}

	// Входы не наследуются подкомандами: без cmd они бессмысленны.
	hasInputs := len(cmd.Args) > 0 || len(cmd.Flags) > 0
	if !hasCmd && hasInputs {
		*errors = append(*errors, fmt.Errorf("command %q: args/flags require cmd", path))
	}

	for _, err := range inputs.Validate(cmd.Args, cmd.Flags) {
		*errors = append(*errors, fmt.Errorf("command %q: %w", path, err))
	}

	if hasCmd {
		tmpl, err := cmd.Cmd.Template()
		if err != nil {
			*errors = append(*errors, fmt.Errorf("command %q: cmd: %w", path, err))
		} else {
			for _, err := range tmpl.Check(inputs.TemplateInputs(cmd.Args, cmd.Flags)) {
				*errors = append(*errors, fmt.Errorf("command %q: cmd: %w", path, err))
			}
		}
	}

	if w, ok := shadowWarning(path, cmd); ok {
		*warnings = append(*warnings, w)
	}
}

// ShadowWarnings returns the warnings of c and of its subcommands (recursively)
// where a subcommand name could be meant as a value: the node has subcommands
// and a positional string arg (variadic and rest included), so a word equal to
// a subcommand name is taken as the subcommand, not as the value. Subcommand
// paths are path plus the command names, separated by spaces.
func ShadowWarnings(path string, c *Command) []string {
	var out []string
	if w, ok := shadowWarning(path, c); ok {
		out = append(out, w)
	}
	for _, name := range sortedNames(c.Commands) {
		out = append(out, ShadowWarnings(path+" "+name, c.Commands[name])...)
	}
	return out
}

// shadowWarning is the warning of the node itself, without descending.
func shadowWarning(path string, c *Command) (string, bool) {
	if len(c.Commands) == 0 {
		return "", false
	}
	for _, a := range c.Args {
		if a.Kind() == argvtmpl.String {
			return fmt.Sprintf("command %q: words %s are subcommands; pass such a value after --", path, strings.Join(sortedNames(c.Commands), ", ")), true
		}
	}
	return "", false
}

func sortedNames(cmds map[string]*Command) []string {
	names := make([]string, 0, len(cmds))
	for name := range cmds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
