package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tivjnb/futi/internal/core"
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
func runHelp(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, rt *core.Runtime, req *helpRequest) (int, error) {
	source, _ := rt.Source() // main has already checked the error
	loader := rt.Cache()
	// Полный путь с "help" как последней командой.
	full := make([]string, len(req.positional)+1)
	copy(full, req.positional)
	full[len(req.positional)] = "help"

	pa := &parsedArgs{flags: req.flags, argsList: req.argsList, dryRun: req.dryRun}
	pa.namespace, pa.command = splitPath(full)
	var resolved bool
	t, err := rt.Resolve(pa.namespace, pa.command)
	switch {
	case err == nil && t.Command().Cmd != "":
		return runTarget(ctx, stdin, stdout, stderr, rt, t, pa) // single module load
	case err == nil || helpMiss(err):
		resolved = true // module loaded, no executable help
	default:
		// Module not found, broken module, source error: the original path.
		var executable bool
		resolved, executable = core.ExecutableHelpIn(source, loader, full)
		if executable {
			return runParsed(ctx, stdin, stdout, stderr, rt, pa)
		}
	}

	if req.dryRun && resolved {
		// Системная справка — не команда модуля: печатать нечего. Для
		// неразрешённого пути отказа нет: нужна обычная ошибка «не найден».
		fmt.Fprintln(stderr, errDryRunScope)
		return 1, nil
	}
	return systemHelpCode(stdout, stderr, source, loader, req.positional), nil
}

// helpMiss reports errors that Resolve returns only after the module was
// loaded and the path had no executable command.
func helpMiss(err error) bool {
	return errors.Is(err, execmodule.ErrCommandNotFound) ||
		errors.Is(err, execmodule.ErrRequiresSubcommand) ||
		errors.Is(err, modulerunner.ErrNoDefaultCommand)
}

// runSystemHelp показывает системную справку для positional:
// модульную (описание + команды) или командную (описание + параметры + cmd).
// Пустой positional (корень) — printUsage.
func runSystemHelp(w io.Writer, source discovery.Source, loader modulerunner.ModuleLoader, positional []string) error {
	doc, err := core.HelpIn(source, loader, positional)
	if err != nil {
		var partial *discovery.PartialError
		if !errors.As(err, &partial) {
			return err
		}
	}
	name := strings.Join(positional, " ")
	switch doc.Kind {
	case core.HelpUsage:
		printUsage(w)
	case core.HelpModule:
		fmt.Fprint(w, execmodule.FormatModuleHelp(doc.Module, name))
	case core.HelpCommand:
		fmt.Fprint(w, execmodule.FormatCommandHelp(name, doc.Command))
	case core.HelpNamespace:
		// Частичный листинг: дети читаемых sources выводятся, ошибка
		// возвращается — вызывающий покажет предупреждения.
		if rerr := renderChildren(w, doc.Children, false); rerr != nil {
			return rerr
		}
	}
	return err
}
