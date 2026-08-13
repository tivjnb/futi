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
// stdout/stderr — опциональные writers для перехвата вывода (nil = os.Stdout/os.Stderr).
func Run(
	ctx context.Context,
	source discovery.Source,
	namespace, command string,
	params map[string]string,
	stdout, stderr io.Writer,
) error {
	manifestPath, moduleRoot, err := source.Resolve(namespace)
	if err != nil {
		return fmt.Errorf("modulerunner: %w", err)
	}

	mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
	if err != nil {
		return fmt.Errorf("modulerunner: %w", err)
	}

	cmd, ok := mod.Commands[command]
	if !ok {
		return fmt.Errorf("modulerunner: command %q not found in module %q", command, namespace)
	}

	resolved, err := cmd.Validate(params)
	if err != nil {
		return fmt.Errorf("modulerunner: %w", err)
	}

	rendered, err := cmd.Render(resolved)
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
	// Если executable содержит "/" и не абсолютный — joined с moduleRoot.
	// exec.LookPath проверяет CWD процесса, а не command.Dir, поэтому
	// резолвим явно для скриптов пакета (./scripts/deploy.sh и т.д.).
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
