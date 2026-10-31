package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// runHelp prints system help for the node at path: `futi help <path...>`.
func runHelp(stdout, stderr io.Writer, rt *core.Runtime, path []string) int {
	source, _ := rt.Source() // main has already checked the error
	return systemHelpCode(stdout, stderr, source, rt.Cache(), path)
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
	name := strings.Join(doc.Path, " ")
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

// systemHelpCode печатает справку по пути в stdout и возвращает код выхода:
// 0 (в том числе при частичном листинге — предупреждения в stderr) или 1 с
// ошибкой в stderr.
func systemHelpCode(stdout, stderr io.Writer, source discovery.Source, loader modulerunner.ModuleLoader, path []string) int {
	if err := runSystemHelp(stdout, source, loader, path); err != nil {
		if printPartialWarnings(stderr, err) {
			return 0
		}
		msg := err.Error()
		if coveredByWarning(err) {
			msg = coveredReadMessage("help", strings.Join(path, "."))
		}
		fmt.Fprintf(stderr, "%s\n", withValidateHint(msg, err, path, source))
		return 1
	}
	return 0
}
