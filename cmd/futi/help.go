package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// runHelp обрабатывает запрос справки req. Сначала проверяет user-defined
// команду "help": если "<positional> help" резолвится как исполнимая
// команда/модуль — выполняет её общим путём runParsed (флаги, args, раздельные
// stdout/stderr, код команды; user-defined выигрывает). Иначе показывает
// системную справку для positional (флаги и args игнорируются). Возвращает код
// выхода и ошибку запуска пользовательской команды (см. runParsed).
func runHelp(ctx context.Context, stdout, stderr io.Writer, source discovery.Source, loader modulerunner.ModuleLoader, req *helpRequest) (int, error) {
	// Полный путь с "help" как последней командой.
	full := make([]string, len(req.positional)+1)
	copy(full, req.positional)
	full[len(req.positional)] = "help"

	if manifest, root, cmdPath, ok := resolvePositional(source, full); ok {
		if isExecutableHelp(source, loader, manifest, root, cmdPath) {
			pa := &parsedArgs{flags: req.flags, args: req.args, argsList: req.argsList}
			pa.namespace, pa.command = splitPath(full)
			return runParsed(ctx, stdout, stderr, source, loader, pa)
		}
	}

	return systemHelpCode(stdout, stderr, source, loader, req.positional), nil
}

// resolvePositional находит модуль для positional через progressive resolution
// (от длиннейшего namespace к коротчайшему). Возвращает манифест, корень и
// оставшийся command path. ok=false если ни один namespace не резолвится.
func resolvePositional(source discovery.Source, positional []string) (manifest, root string, cmdPath []string, ok bool) {
	for i := len(positional); i >= 0; i-- {
		ns := strings.Join(positional[:i], ".")
		mp, mr, err := source.Resolve(ns)
		if err == nil {
			return mp, mr, positional[i:], true
		}
	}
	return "", "", nil, false
}

// isExecutableHelp определяет, резолвится ли командный путь как исполнимая
// команда "help" (листовая с Cmd) или модуль с default cmd. Возвращает false
// для веток без Cmd и несуществующих команд — тогда показываем системную справку.
func isExecutableHelp(source discovery.Source, loader modulerunner.ModuleLoader, manifest, root string, cmdPath []string) bool {
	mod, err := modulerunner.LoadModule(loader, manifest, root)
	if err != nil {
		return false
	}
	if len(cmdPath) == 0 {
		return mod.Cmd != "" // модуль с default cmd (например модуль "help")
	}
	cmd, err := mod.ResolveCommandPath(cmdPath)
	if err != nil {
		return false
	}
	return cmd.Cmd != ""
}

// helpModuleNotFoundError — модуль для справки не найден; Unwrap отдаёт
// sentinel modulerunner.ErrModuleNotFound (для подсказки про validate).
type helpModuleNotFoundError struct{ path string }

func (e *helpModuleNotFoundError) Error() string {
	return fmt.Sprintf("help: module not found for %q", e.path)
}

func (e *helpModuleNotFoundError) Unwrap() error { return modulerunner.ErrModuleNotFound }

// runSystemHelp показывает системную справку для positional:
// модульную (описание + команды) или командную (описание + параметры + cmd).
// Пустой positional (корень) — printUsage.
func runSystemHelp(w io.Writer, source discovery.Source, loader modulerunner.ModuleLoader, positional []string) error {
	if len(positional) == 0 {
		printUsage(w)
		return nil
	}

	manifest, root, cmdPath, ok := resolvePositional(source, positional)
	if !ok {
		// Не модуль — возможно, каталог-namespace: справка = его дочерние элементы
		// (как `futi <ns> list`).
		children, err := source.ListChildren(strings.Join(positional, "."))
		var partial *discovery.PartialError
		isPartial := errors.As(err, &partial)
		if err != nil && !isPartial && !errors.Is(err, discovery.ErrNotFound) {
			return fmt.Errorf("help: %w", err)
		}
		if (err == nil || isPartial) && len(children) > 0 {
			// Частичный листинг: дети читаемых sources выводятся, ошибка
			// возвращается — вызывающий покажет предупреждения.
			if perr := printChildren(w, children, false, loader); perr != nil {
				return perr
			}
			return err
		}
		return &helpModuleNotFoundError{path: strings.Join(positional, " ")}
	}

	mod, err := modulerunner.LoadModule(loader, manifest, root)
	if err != nil {
		return fmt.Errorf("help: %w", err)
	}

	// Модульный help: command path пуст.
	if len(cmdPath) == 0 {
		fmt.Fprint(w, execmodule.FormatModuleHelp(mod, strings.Join(positional, " ")))
		return nil
	}

	// Командный help: резолвим command path.
	cmd, err := mod.ResolveCommandPath(cmdPath)
	if err != nil {
		return fmt.Errorf("help: %w", err)
	}

	// Ветка с subcommands — справка ветки (как модульная, по subcommands).
	if len(cmd.Commands) > 0 {
		branch := &execmodule.Module{
			Name:        mod.Name,
			Description: cmd.Description,
			Cmd:         cmd.Cmd,
			Params:      cmd.Params,
			Positional:  cmd.Positional,
			Commands:    cmd.Commands,
		}
		fmt.Fprint(w, execmodule.FormatModuleHelp(branch, strings.Join(positional, " ")))
		return nil
	}

	// Листовая команда.
	fmt.Fprint(w, execmodule.FormatCommandHelp(strings.Join(positional, " "), cmd))
	return nil
}
