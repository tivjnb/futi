package execmodule

import (
	"fmt"
	"regexp"
	"strings"
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

// Validate проверяет схему модуля и возвращает все найденные ошибки и предупреждения.
//
// Errors (блокируют загрузку модуля в LoadModule):
//   - команда без cmd и без commands (бесполезная команда)
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
func (m *Module) Validate() (errors []error, warnings []string) {
	if m.Cmd != "" {
		validateCommand("(default)", &Command{
			Cmd:        m.Cmd,
			Params:     m.Params,
			Positional: m.Positional,
		}, &errors, &warnings)
	}
	for name, cmd := range m.Commands {
		validateCommand(name, cmd, &errors, &warnings)
	}
	return errors, warnings
}

// validateCommand рекурсивно проверяет команду и её subcommands.
// path — имя команды (для сообщений об ошибках).
func validateCommand(path string, cmd *Command, errors *[]error, warnings *[]string) {
	// Проверка: команда должна иметь cmd или commands (или оба).
	hasCmd := cmd.Cmd != ""
	hasCommands := len(cmd.Commands) > 0

	if !hasCmd && !hasCommands {
		*errors = append(*errors, fmt.Errorf("command %q: must have cmd or commands", path))
	}

	// Предупреждение: команда с commands, но без cmd — вызов без subcommand падает.
	if hasCommands && !hasCmd {
		*warnings = append(*warnings, fmt.Sprintf("command %q: has subcommands but no cmd (calling without subcommand will fail)", path))
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

	// Рекурсивная проверка subcommands.
	for subName, subCmd := range cmd.Commands {
		validateCommand(path+"."+subName, subCmd, errors, warnings)
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
