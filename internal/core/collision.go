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

// PathCollisions finds paths of the merged tree of src that address two
// entities: a command of a module whose path is also a module or a namespace
// directory (V4). Checked are the modules under namespace (the module itself
// or a module below it; empty = all) and their ancestor modules, whose
// commands reach an address under namespace. Every collision is a
// *modulerunner.CollisionError, in the order of module namespaces and command
// paths. A partial listing of src is used as is; modules that cannot be
// resolved or loaded are skipped, their problems are reported by their own
// validation. The returned error is a failure to list src.
func PathCollisions(src discovery.Source, loader modulerunner.ModuleLoader, namespace string) ([]error, error) {
	return pathCollisions(src, namespace, func(manifest, root string) (*execmodule.Module, error) {
		return modulerunner.LoadModule(loader, manifest, root)
	})
}

// pathCollisions is PathCollisions with a custom module load function.
func pathCollisions(src discovery.Source, namespace string, load func(manifest, root string) (*execmodule.Module, error)) ([]error, error) {
	addrs, err := treeAddrs(src)
	if err != nil {
		return nil, err
	}
	var moduleNs []string
	for addr, isModule := range addrs {
		if isModule {
			moduleNs = append(moduleNs, addr)
		}
	}
	var mods []loadedModule
	for _, ns := range moduleNs {
		if !inScope(ns, namespace) && !isAncestor(ns, namespace) {
			continue
		}
		manifest, root, err := src.Resolve(ns)
		if err != nil {
			continue
		}
		mod, err := load(manifest, root)
		if err != nil {
			continue
		}
		mods = append(mods, loadedModule{ns: ns, manifest: manifest, mod: mod})
	}
	return collisionsOf(src, addrs, mods, namespace), nil
}

// loadedModule is a module that is checked for collisions.
type loadedModule struct {
	ns, manifest string
	mod          *execmodule.Module
}

// inScope reports whether address ns is namespace or lies below it
// (empty namespace = the whole tree).
func inScope(ns, namespace string) bool {
	return namespace == "" || ns == namespace || strings.HasPrefix(ns, namespace+".")
}

// isAncestor reports whether ns is a proper prefix of namespace.
func isAncestor(ns, namespace string) bool {
	return strings.HasPrefix(namespace, ns+".")
}

// treeAddrs returns the addresses of the merged tree of src: namespaces of
// all modules and all their prefixes. A module address maps to true, a pure
// namespace directory to false. A partial listing is used as is.
func treeAddrs(src discovery.Source) (map[string]bool, error) {
	infos, err := src.List()
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return nil, err
	}
	addrs := map[string]bool{}
	for _, info := range infos {
		addrs[info.Name] = true
		segs := strings.Split(info.Name, ".")
		for i := 1; i < len(segs); i++ {
			prefix := strings.Join(segs[:i], ".")
			if _, ok := addrs[prefix]; !ok {
				addrs[prefix] = false
			}
		}
	}
	return addrs, nil
}

// collisionsOf finds the commands of mods whose address is in addrs and lies
// under namespace. mods are processed in the order of their namespaces.
func collisionsOf(src discovery.Source, addrs map[string]bool, mods []loadedModule, namespace string) []error {
	sort.Slice(mods, func(i, j int) bool { return mods[i].ns < mods[j].ns })
	var out []error
	for _, m := range mods {
		for _, p := range commandPaths(nil, m.mod.Commands) {
			addr := m.ns + "." + strings.Join(p, ".")
			isModule, ok := addrs[addr]
			if !ok || !inScope(addr, namespace) {
				continue
			}
			ce := &modulerunner.CollisionError{
				Path: append(strings.Split(m.ns, "."), p...),
				A:    modulerunner.DescribeCommand(m.ns, p, m.manifest),
				B:    modulerunner.DescribeNamespace(addr),
			}
			if isModule {
				other, _, err := src.Resolve(addr)
				if err != nil {
					continue
				}
				ce.B = modulerunner.DescribeModule(addr, other)
			}
			out = append(out, ce)
		}
	}
	return out
}

// commandPaths lists the paths of all commands in cmds, a parent before its
// nested commands, names sorted.
func commandPaths(prefix []string, cmds map[string]*execmodule.Command) [][]string {
	names := make([]string, 0, len(cmds))
	for name := range cmds {
		names = append(names, name)
	}
	sort.Strings(names)
	var out [][]string
	for _, name := range names {
		p := append(append([]string(nil), prefix...), name)
		out = append(out, p)
		out = append(out, commandPaths(p, cmds[name].Commands)...)
	}
	return out
}

// collisionTexts renders collision errors as warning texts.
func collisionTexts(errs []error) []string {
	var out []string
	for _, e := range errs {
		out = append(out, fmt.Sprint(e))
	}
	return out
}
