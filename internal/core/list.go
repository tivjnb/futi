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
	Warnings   []string       // path collisions found in the listed subtree (error texts)

	loaded []loadedModule // modules loaded for this listing, to check for path collisions
}

// ListIn lists namespace in src as seen from f (a hidden module or command
// is a *NotExposedError): a module's commands, a command's
// subcommands, or the children of a namespace (the whole subtree if recurse).
// For a partial listing it returns both a valid result and the
// *discovery.PartialError. Other errors are classified.
func ListIn(f execmodule.Frontend, src discovery.Source, loader modulerunner.ModuleLoader, namespace string, recurse bool) (ListResult, error) {
	res, err := listIn(f, src, loader, namespace, recurse)
	if err == nil || errors.As(err, new(*discovery.PartialError)) {
		res.Warnings = collisionWarnings(src, res.loaded)
	}
	return res, classifyUnlessPartial(err)
}

// collisionWarnings checks the modules the listing has loaded anyway for
// path collisions; no module is loaded for the check.
func collisionWarnings(src discovery.Source, loaded []loadedModule) []string {
	if len(loaded) == 0 {
		return nil
	}
	addrs, err := treeAddrs(src)
	if err != nil {
		return []string{"cannot check path collisions: " + err.Error()}
	}
	return collisionTexts(collisionsOf(src, addrs, loaded, ""))
}

func listIn(f execmodule.Frontend, src discovery.Source, loader modulerunner.ModuleLoader, namespace string, recurse bool) (ListResult, error) {
	// Сначала пробуем резолвить как модуль. Пустой namespace — запрос корня:
	// Resolve("") вернул бы «invalid namespace», а не ErrNotFound.
	if namespace != "" {
		manifestPath, moduleRoot, err := src.Resolve(namespace)
		if err == nil {
			res, mod, err := moduleCommands(f, loader, manifestPath, moduleRoot)
			if err == nil {
				if err := notExposed(f, strings.Split(namespace, "."), &mod.Command); err != nil {
					return ListResult{}, err
				}
				res.loaded = []loadedModule{{ns: namespace, manifest: manifestPath, mod: mod}}
			}
			return res, err
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

			if err := notExposed(f, append(strings.Split(ns, "."), commandPath...), cmd); err != nil {
				return ListResult{}, err
			}

			if len(cmd.Commands) > 0 {
				res := commandsResult(f, ListSubcommands, cmd.Commands, manifestPath, !cmd.Cmd.IsZero())
				res.loaded = []loadedModule{{ns: ns, manifest: manifestPath, mod: mod}}
				return res, nil
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
	// Partial listing: the result is valid, return it together with the error.
	var partial *discovery.PartialError
	if listErr != nil && !errors.As(listErr, &partial) {
		return ListResult{}, fmt.Errorf("list: %w", listErr)
	}
	entries, loaded := describeChildren(f, children, loader, namespace)
	res := ListResult{Kind: ListNamespace, Children: entries, loaded: loaded}
	if listErr != nil {
		return res, listErr
	}
	return res, nil
}

// ModuleCommands loads the module and lists its top-level commands.
func ModuleCommands(f execmodule.Frontend, loader modulerunner.ModuleLoader, manifestPath, moduleRoot string) (ListResult, error) {
	res, _, err := moduleCommands(f, loader, manifestPath, moduleRoot)
	return res, err
}

func moduleCommands(f execmodule.Frontend, loader modulerunner.ModuleLoader, manifestPath, moduleRoot string) (ListResult, *execmodule.Module, error) {
	mod, err := modulerunner.LoadModule(loader, manifestPath, moduleRoot)
	if err != nil {
		return ListResult{}, nil, fmt.Errorf("list: %w", err)
	}
	return commandsResult(f, ListModule, mod.Commands, manifestPath, !mod.Cmd.IsZero()), mod, nil
}

func commandsResult(f execmodule.Frontend, kind ListKind, commands map[string]*execmodule.Command, manifestPath string, hasDefault bool) ListResult {
	res := ListResult{Kind: kind, Manifest: manifestPath, HasDefault: hasDefault}
	for name, cmd := range commands {
		if !cmd.ExposedTo(f) {
			continue
		}
		res.Commands = append(res.Commands, CommandEntry{Name: name, Description: cmd.Description})
	}
	sort.Slice(res.Commands, func(i, j int) bool { return res.Commands[i].Name < res.Commands[j].Name })
	return res
}

// DescribeChildren converts discovery children into entries, loading the
// description of every non-conflicting module through loader (not verified).
// A loaded module hidden from f is left out; a module that fails to load stays.
func DescribeChildren(f execmodule.Frontend, children []discovery.Child, loader modulerunner.ModuleLoader) []ChildEntry {
	entries, _ := describeChildren(f, children, loader, "")
	return entries
}

// describeChildren is DescribeChildren that also returns the loaded modules.
// Child names are relative to namespace.
func describeChildren(f execmodule.Frontend, children []discovery.Child, loader modulerunner.ModuleLoader, namespace string) ([]ChildEntry, []loadedModule) {
	var loaded []loadedModule
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
			// Resolve of a conflicting name returns an error: do not load the description.
			if !c.Conflict {
				if mod, err := modulerunner.LoadModule(loader, c.ManifestPath, c.ModuleRoot); err != nil {
					e.LoadErr = err
				} else {
					e.Description = mod.Description
					ns := c.Name
					if namespace != "" {
						ns = namespace + "." + c.Name
					}
					// Hidden modules still take part in collision checks.
					loaded = append(loaded, loadedModule{ns: ns, manifest: c.ManifestPath, mod: mod})
					if !mod.ExposedTo(f) {
						continue
					}
				}
			}
		}
		entries = append(entries, e)
	}
	return entries, loaded
}

// List lists namespace in the runtime's source, see ListIn.
func (r *Runtime) List(f execmodule.Frontend, namespace string, recurse bool) (ListResult, error) {
	src, err := r.Source()
	if err != nil {
		return ListResult{}, err
	}
	return ListIn(f, src, r.loader(), namespace, recurse)
}
