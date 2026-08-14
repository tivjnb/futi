package modulerunner

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"futi/internal/discovery"
	"futi/internal/execmodule"
	"futi/internal/executor"

	"github.com/google/shlex"
)

// Run находит модуль через source, загружает его, валидирует параметры,
// рендерит cmd-шаблон и выполняет команду.
// Поддерживает subcommands: progressive resolution пробует резолвить модуль
// от длиннейшего namespace к коротчайшему; остаток = command path.
// args — строка после `--` (для {{.args}}), argsList — массив (для {{.args_list}}).
// command="" означает вызов default cmd модуля (Module.Cmd).
// stdout/stderr — опциональные writers для перехвата вывода (nil = os.Stdout/os.Stderr).
func Run(
	ctx context.Context,
	source discovery.Source,
	namespace, command string,
	params map[string]string,
	args string,
	argsList []string,
	stdout, stderr io.Writer,
) error {
	// Реконструкция positional args из namespace + command.
	var positional []string
	if namespace == "" {
		positional = []string{command}
	} else if command == "" {
		positional = []string{namespace}
	} else {
		positional = strings.Split(namespace, ".")
		positional = append(positional, command)
	}

	// Progressive resolution: пробуем резолвить модуль от длиннейшего
	// namespace к коротчайшему. Остаток = command path.
	// i=len(positional) → ns=все, commandPath=[] (default cmd случай).
	for i := len(positional); i >= 0; i-- {
		ns := strings.Join(positional[:i], ".")
		commandPath := positional[i:]

		manifestPath, moduleRoot, err := source.Resolve(ns)
		if err != nil {
			continue
		}

		mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
		if err != nil {
			return fmt.Errorf("modulerunner: %w", err)
		}

		// commandPath пустой → default cmd модуля
		var cmd *execmodule.Command
		if len(commandPath) == 0 {
			if mod.Cmd == "" {
				return fmt.Errorf("modulerunner: module %q has no default command; specify one", ns)
			}
			cmd = &execmodule.Command{Cmd: mod.Cmd}
		} else {
			cmd, err = mod.ResolveCommand(commandPath)
			if err != nil {
				return fmt.Errorf("modulerunner: %w", err)
			}
		}

		resolved, err := cmd.Validate(params)
		if err != nil {
			return fmt.Errorf("modulerunner: %w", err)
		}

		rendered, err := cmd.Render(resolved, args, argsList)
		if err != nil {
			return fmt.Errorf("modulerunner: %w", err)
		}

		parts, err := shlex.Split(rendered)
		if err != nil {
			return fmt.Errorf("modulerunner: split command %q: %w", rendered, err)
		}
		if len(parts) == 0 {
			return fmt.Errorf("modulerunner: empty command after render")
		}

		// Резолвинг относительных путей к скриптам пакета от moduleRoot.
		if !filepath.IsAbs(parts[0]) && strings.Contains(parts[0], "/") {
			parts[0] = filepath.Join(moduleRoot, parts[0])
		}

		cfg := executor.NewConfig().WithDir(moduleRoot)
		if stdout != nil {
			cfg = cfg.WithStdout(stdout)
		}
		if stderr != nil {
			cfg = cfg.WithStderr(stderr)
		}

		return executor.RunWithConfig(ctx, cfg, parts[0], parts[1:]...)
	}

	return fmt.Errorf("modulerunner: module not found for %v", positional)
}

// CommandInfo описывает команду модуля для листинга.
type CommandInfo struct {
	Name        string
	Description string
}

// ListCommands находит модуль через source, загружает его и возвращает
// список команд с описаниями.
func ListCommands(source discovery.Source, namespace string) ([]CommandInfo, error) {
	manifestPath, moduleRoot, err := source.Resolve(namespace)
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	cmds := make([]CommandInfo, 0, len(mod.Commands))
	for name, cmd := range mod.Commands {
		cmds = append(cmds, CommandInfo{
			Name:        name,
			Description: cmd.Description,
		})
	}
	return cmds, nil
}
