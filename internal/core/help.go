package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// HelpKind says what a HelpDoc describes.
type HelpKind int

const (
	HelpUsage     HelpKind = iota // empty path: general usage
	HelpModule                    // module, or a branch with subcommands (Module is synthetic)
	HelpCommand                   // leaf command
	HelpNamespace                 // namespace directory: its children
)

// HelpDoc is the data of one system help request; which fields are set
// depends on Kind.
type HelpDoc struct {
	Kind     HelpKind
	Module   *execmodule.Module  // HelpModule
	Command  *execmodule.Command // HelpCommand
	Children []ChildEntry        // HelpNamespace
}

// HelpIn resolves system help for path in src. For a partial namespace
// listing it returns both a valid HelpNamespace and the *discovery.PartialError.
// Other errors are classified.
func HelpIn(src discovery.Source, loader modulerunner.ModuleLoader, path []string) (HelpDoc, error) {
	doc, err := helpIn(src, loader, path)
	return doc, classifyUnlessPartial(err)
}

func helpIn(src discovery.Source, loader modulerunner.ModuleLoader, path []string) (HelpDoc, error) {
	if len(path) == 0 {
		return HelpDoc{Kind: HelpUsage}, nil
	}

	manifest, root, cmdPath, ok := resolvePositional(src, path)
	if !ok {
		// Не модуль — возможно, каталог-namespace: справка = его дочерние элементы
		// (как `futi <ns> list`).
		children, err := src.ListChildren(strings.Join(path, "."))
		var partial *discovery.PartialError
		isPartial := errors.As(err, &partial)
		if err != nil && !isPartial && !errors.Is(err, discovery.ErrNotFound) {
			return HelpDoc{}, fmt.Errorf("help: %w", err)
		}
		if (err == nil || isPartial) && len(children) > 0 {
			// Частичный листинг: дети читаемых sources возвращаются вместе с
			// ошибкой — вызывающий покажет предупреждения.
			return HelpDoc{Kind: HelpNamespace, Children: DescribeChildren(children, loader)}, err
		}
		return HelpDoc{}, &helpModuleNotFoundError{path: strings.Join(path, " ")}
	}

	mod, err := modulerunner.LoadModuleVerified(loader, manifest, root)
	if err != nil {
		return HelpDoc{}, fmt.Errorf("help: %w", err)
	}

	// Модульный help: command path пуст.
	if len(cmdPath) == 0 {
		return HelpDoc{Kind: HelpModule, Module: mod}, nil
	}

	// Командный help: резолвим command path.
	cmd, err := mod.ResolveCommandPath(cmdPath)
	if err != nil {
		return HelpDoc{}, fmt.Errorf("help: %w", err)
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
		return HelpDoc{Kind: HelpModule, Module: branch}, nil
	}

	// Листовая команда.
	return HelpDoc{Kind: HelpCommand, Command: cmd}, nil
}

// ExecutableHelpIn reports whether path (ending in "help") resolves to a module
// and whether that is a runnable user command or a module default command.
func ExecutableHelpIn(src discovery.Source, loader modulerunner.ModuleLoader, path []string) (resolved, executable bool) {
	manifest, root, cmdPath, ok := resolvePositional(src, path)
	if !ok {
		return false, false
	}
	return true, isExecutableHelp(loader, manifest, root, cmdPath)
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
func isExecutableHelp(loader modulerunner.ModuleLoader, manifest, root string, cmdPath []string) bool {
	mod, err := modulerunner.LoadModuleVerified(loader, manifest, root)
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

// Help resolves system help for path in the runtime's source, see HelpIn.
func (r *Runtime) Help(path []string) (HelpDoc, error) {
	src, err := r.Source()
	if err != nil {
		return HelpDoc{}, err
	}
	return HelpIn(src, r.loader(), path)
}
