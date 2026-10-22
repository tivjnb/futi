package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// ListKind says what a ListResult describes.
type ListKind int

const (
	ListModule      ListKind = iota // commands of a module
	ListSubcommands                 // subcommands of a command inside a module
	ListNamespace                   // children of a namespace
)

// CommandEntry is one command of a module or subcommand listing.
type CommandEntry struct{ Name, Description string }

// ChildEntry is one child of a namespace listing. Text fields are raw.
type ChildEntry struct {
	Name            string   // discovery.Child.Name as is
	IsModule        bool     // Type == "module"; otherwise "namespace"
	Path            string   // manifest path (modules only)
	Description     string   // module description; empty for conflicts and load errors
	Conflict        bool     // Resolve of this name fails with a conflict error
	ConflictSources []string // RootDirs of sources with the same module; empty for x.yaml vs x/config.yaml
	LoadErr         error    // module failed to load; Description is empty
}

// ListResult is the data of one listing; which fields are set depends on Kind.
type ListResult struct {
	Kind       ListKind
	Manifest   string         // ListModule, ListSubcommands: manifest path
	HasDefault bool           // ListModule: module has cmd; ListSubcommands: command has cmd
	Commands   []CommandEntry // sorted by Name
	Children   []ChildEntry   // ListNamespace: in discovery order
}

// ListIn lists namespace in src: a module's commands, a command's
// subcommands, or the children of a namespace (the whole subtree if recurse).
// For a partial listing it returns both a valid result and the
// *discovery.PartialError. Other errors are classified.
func ListIn(src discovery.Source, loader modulerunner.ModuleLoader, namespace string, recurse bool) (ListResult, error) {
	res, err := listIn(src, loader, namespace, recurse)
	return res, classifyUnlessPartial(err)
}

func listIn(src discovery.Source, loader modulerunner.ModuleLoader, namespace string, recurse bool) (ListResult, error) {
	// Сначала пробуем резолвить как модуль. Пустой namespace — запрос корня:
	// Resolve("") вернул бы «invalid namespace», а не ErrNotFound.
	if namespace != "" {
		manifestPath, moduleRoot, err := src.Resolve(namespace)
		if err == nil {
			return ModuleCommands(loader, manifestPath, moduleRoot)
		}
		if !errors.Is(err, discovery.ErrNotFound) {
			return ListResult{}, fmt.Errorf("list: %w", err)
		}
	}

	// Не модуль — пробуем progressive resolution для subcommands
	if namespace != "" {
		segments := strings.Split(namespace, ".")
		for i := len(segments) - 1; i > 0; i-- {
			ns := strings.Join(segments[:i], ".")
			commandPath := segments[i:]

			manifestPath, moduleRoot, err := src.Resolve(ns)
			if err != nil {
				if errors.Is(err, discovery.ErrNotFound) {
					continue
				}
				return ListResult{}, fmt.Errorf("list: %w", err)
			}

			mod, err := modulerunner.LoadModule(loader, manifestPath, moduleRoot)
			if err != nil {
				return ListResult{}, fmt.Errorf("list: %w", err)
			}

			cmd, err := mod.ResolveCommandPath(commandPath)
			if err != nil {
				return ListResult{}, fmt.Errorf("list: %w", err)
			}

			if len(cmd.Commands) > 0 {
				return commandsResult(ListSubcommands, cmd.Commands, manifestPath, cmd.Cmd), nil
			}

			return ListResult{}, fmt.Errorf("list: command %q has no subcommands", strings.Join(commandPath, " "))
		}
	}

	// Не модуль и не subcommand — пробуем как namespace
	var children []discovery.Child
	var listErr error
	if recurse {
		children, listErr = src.ListChildrenRecursive(namespace)
	} else {
		children, listErr = src.ListChildren(namespace)
	}
	// Частичный листинг: результат валиден, возвращаем его вместе с ошибкой.
	var partial *discovery.PartialError
	if listErr != nil && !errors.As(listErr, &partial) {
		return ListResult{}, fmt.Errorf("list: %w", listErr)
	}
	res := ListResult{Kind: ListNamespace, Children: DescribeChildren(children, loader)}
	if listErr != nil {
		return res, listErr
	}
	return res, nil
}

// ModuleCommands loads the module and lists its top-level commands.
func ModuleCommands(loader modulerunner.ModuleLoader, manifestPath, moduleRoot string) (ListResult, error) {
	mod, err := modulerunner.LoadModule(loader, manifestPath, moduleRoot)
	if err != nil {
		return ListResult{}, fmt.Errorf("list: %w", err)
	}
	return commandsResult(ListModule, mod.Commands, manifestPath, mod.Cmd), nil
}

func commandsResult(kind ListKind, commands map[string]*execmodule.Command, manifestPath, defaultCmd string) ListResult {
	res := ListResult{Kind: kind, Manifest: manifestPath, HasDefault: defaultCmd != ""}
	for name, cmd := range commands {
		res.Commands = append(res.Commands, CommandEntry{Name: name, Description: cmd.Description})
	}
	sort.Slice(res.Commands, func(i, j int) bool { return res.Commands[i].Name < res.Commands[j].Name })
	return res
}

// DescribeChildren converts discovery children into entries, loading the
// description of every non-conflicting module through loader (not verified).
func DescribeChildren(children []discovery.Child, loader modulerunner.ModuleLoader) []ChildEntry {
	entries := make([]ChildEntry, 0, len(children))
	for _, c := range children {
		e := ChildEntry{
			Name:            c.Name,
			IsModule:        c.Type == "module",
			Conflict:        c.Conflict,
			ConflictSources: c.ConflictSources,
		}
		if e.IsModule {
			e.Path = c.ManifestPath
			// Resolve для конфликтного имени вернёт ошибку: описание не грузим.
			if !c.Conflict {
				if mod, err := modulerunner.LoadModule(loader, c.ManifestPath, c.ModuleRoot); err != nil {
					e.LoadErr = err
				} else {
					e.Description = mod.Description
				}
			}
		}
		entries = append(entries, e)
	}
	return entries
}

// List lists namespace in the runtime's source, see ListIn.
func (r *Runtime) List(namespace string, recurse bool) (ListResult, error) {
	src, err := r.Source()
	if err != nil {
		return ListResult{}, err
	}
	return ListIn(src, r.loader(), namespace, recurse)
}
