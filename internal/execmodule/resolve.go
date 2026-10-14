package execmodule

import (
	"errors"
	"fmt"
	"strings"
)

// ErrCommandNotFound — путь команды не найден в модуле (нет команды/подкоманды
// или у промежуточной команды нет подкоманд). Ошибки резолва оборачивают его
// так, чтобы вызывающий отличал "не найдено" через errors.Is.
var ErrCommandNotFound = errors.New("command not found")

// notFoundError несёт готовый текст ошибки и совпадает с ErrCommandNotFound
// по errors.Is (текст при этом не меняется).
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string        { return e.msg }
func (e *notFoundError) Is(target error) bool { return target == ErrCommandNotFound }

func notFound(format string, args ...any) error {
	return &notFoundError{msg: fmt.Sprintf(format, args...)}
}

// ResolveCommandPath обходит путь команд и возвращает команду по указанному пути.
// В отличие от ResolveCommand, не требует, чтобы конечная команда была листом
// (имела Cmd). Используется для листинга commands.
func (m *Module) ResolveCommandPath(path []string) (*Command, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("execmodule: no command specified in module %q", m.Name)
	}

	cmd, ok := m.Commands[path[0]]
	if !ok {
		return nil, notFound("execmodule: command %q not found in module %q", path[0], m.Name)
	}

	for i := 1; i < len(path); i++ {
		if len(cmd.Commands) == 0 {
			return nil, notFound("execmodule: command %q has no subcommands", strings.Join(path[:i], " "))
		}
		sub, ok := cmd.Commands[path[i]]
		if !ok {
			return nil, notFound("execmodule: subcommand %q not found in %q", path[i], strings.Join(path[:i], " "))
		}
		cmd = sub
	}

	return cmd, nil
}

// ResolveCommand обходит путь команд через commands.
// Возвращает команду с Cmd (готовую к выполнению) или ошибку, если:
//   - путь пуст
//   - команда не найдена
//   - промежуточная команда не имеет commands
//   - конечная команда не имеет cmd (требует subcommand)
func (m *Module) ResolveCommand(path []string) (*Command, error) {
	cmd, err := m.ResolveCommandPath(path)
	if err != nil {
		return nil, err
	}

	if cmd.Cmd == "" && len(cmd.Commands) > 0 {
		return nil, fmt.Errorf("execmodule: command %q requires a subcommand", strings.Join(path, " "))
	}

	return cmd, nil
}
