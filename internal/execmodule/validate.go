package execmodule

import (
	"fmt"
	"strings"
)

// Допустимые типы параметров. Пустой тип означает string (по умолчанию).
var validParamTypes = map[string]bool{
	"":      true,
	"string": true,
	"bool":   true,
	"int":    true,
}

// Validate проверяет схему модуля и возвращает все найденные ошибки и предупреждения.
//
// Errors (блокируют загрузку модуля в LoadModule):
//   - команда без cmd и без commands (бесполезная команда)
//   - дубликаты имён params в команде
//   - type параметра ∉ {"", "string", "bool", "int"}
//
// Warnings (не блокируют, показываются в futi validate):
//   - команда с commands, но без cmd (вызов без subcommand падает в runtime)
//
// Рекурсивно обходит все команды и subcommands. Возвращает ВСЕ ошибки за один
// проход — автор модуля исправляет всё за итерацию, а не по одной ошибке.
func (m *Module) Validate() (errors []error, warnings []string) {
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

	// Проверка params: дубликаты имён и невалидные типы.
	seen := make(map[string]bool)
	for _, p := range cmd.Params {
		if seen[p.Name] {
			*errors = append(*errors, fmt.Errorf("command %q: duplicate param %q", path, p.Name))
		}
		seen[p.Name] = true

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
