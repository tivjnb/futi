package execmodule

import (
	"fmt"
	"strings"
)

// ResolveCommandPath обходит путь команд и возвращает команду по указанному пути.
// В отличие от ResolveCommand, не требует, чтобы конечная команда была листом
// (имела Cmd). Используется для листинга commands.
func (m *Module) ResolveCommandPath(path []string) (*Command, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("execmodule: no command specified in module %q", m.Name)
	}

	cmd, ok := m.Commands[path[0]]
	if !ok {
		return nil, fmt.Errorf("execmodule: command %q not found in module %q", path[0], m.Name)
	}

	for i := 1; i < len(path); i++ {
		if len(cmd.Commands) == 0 {
			return nil, fmt.Errorf("execmodule: command %q has no subcommands", strings.Join(path[:i], " "))
		}
		sub, ok := cmd.Commands[path[i]]
		if !ok {
			return nil, fmt.Errorf("execmodule: subcommand %q not found in %q", path[i], strings.Join(path[:i], " "))
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
