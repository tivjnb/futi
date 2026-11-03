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
	Path     []string            // matched path segments (dotted words split)
	Module   *execmodule.Module  // HelpModule
	Command  *execmodule.Command // HelpCommand
	Children []ChildEntry        // HelpNamespace
}

// HelpIn resolves system help for the node at path in src as seen from f (a
// hidden node is a *NotExposedError); every word of path must be a path
// word. For a partial namespace listing it returns both a valid
// HelpNamespace and the *discovery.PartialError. Other errors are classified.
func HelpIn(f execmodule.Frontend, src discovery.Source, loader modulerunner.ModuleLoader, path []string) (HelpDoc, error) {
	doc, err := helpIn(f, src, loader, path)
	return doc, classifyUnlessPartial(err)
}

func helpIn(f execmodule.Frontend, src discovery.Source, loader modulerunner.ModuleLoader, path []string) (HelpDoc, error) {
	if len(path) == 0 {
		return HelpDoc{Kind: HelpUsage}, nil
	}

	// Help shows what would run: modules are loaded verified, as for execution.
	node, n, err := modulerunner.Walk(src, loader, path, modulerunner.WalkOptions{Verified: true, Frontend: f})
	if err != nil {
		var collision *modulerunner.CollisionError
		if errors.As(err, &collision) {
			return HelpDoc{}, err
		}
		return HelpDoc{}, fmt.Errorf("help: %w", err)
	}
	// A hidden node reached before an unknown word is reported as hidden, as
	// ResolveWords does.
	if node.Command != nil {
		if err := notExposed(f, node.Path, node.Command); err != nil {
			return HelpDoc{}, err
		}
	}
	if n < len(path) {
		return HelpDoc{}, modulerunner.UnknownCommandError(src, node, path[n])
	}

	if node.Module == nil {
		// Namespace directory: its children.
		children, err := src.ListChildren(node.Namespace)
		var partial *discovery.PartialError
		isPartial := errors.As(err, &partial)
		if err != nil && !isPartial && !errors.Is(err, discovery.ErrNotFound) {
			return HelpDoc{}, fmt.Errorf("help: %w", err)
		}
		if (err == nil || isPartial) && len(children) > 0 {
			// Частичный листинг: дети читаемых sources возвращаются вместе с
			// ошибкой — вызывающий покажет предупреждения.
			return HelpDoc{Kind: HelpNamespace, Path: node.Path, Children: DescribeChildren(f, children, loader)}, err
		}
		return HelpDoc{}, &helpModuleNotFoundError{path: strings.Join(node.Path, " ")}
	}

	// Модульный help: command path пуст.
	if len(node.CmdPath) == 0 {
		return HelpDoc{Kind: HelpModule, Path: node.Path, Module: node.Module}, nil
	}

	// Ветка с subcommands — справка ветки (как модульная, по subcommands).
	cmd := node.Command
	if len(cmd.Commands) > 0 {
		branch := &execmodule.Module{Name: node.Module.Name, Command: *cmd}
		return HelpDoc{Kind: HelpModule, Path: node.Path, Module: branch}, nil
	}

	// Листовая команда.
	return HelpDoc{Kind: HelpCommand, Path: node.Path, Command: cmd}, nil
}

// helpModuleNotFoundError — модуль для справки не найден; Unwrap отдаёт
// sentinel modulerunner.ErrModuleNotFound (для подсказки про validate).
type helpModuleNotFoundError struct{ path string }

func (e *helpModuleNotFoundError) Error() string {
	return fmt.Sprintf("help: module not found for %q", e.path)
}

func (e *helpModuleNotFoundError) Unwrap() error { return modulerunner.ErrModuleNotFound }

// Help resolves system help for path in the runtime's source, see HelpIn.
func (r *Runtime) Help(f execmodule.Frontend, path []string) (HelpDoc, error) {
	src, err := r.Source()
	if err != nil {
		return HelpDoc{}, err
	}
	return HelpIn(f, src, r.loader(), path)
}
