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
// entities: a command of a module under namespace (the module itself or a
// module below it; empty = all) whose path matches a module or a namespace
// directory (V4). Every collision is a *modulerunner.CollisionError, in the
// order of module namespaces and command paths. A partial listing of src is
// used as is; modules that cannot be resolved or loaded are skipped, their
// problems are reported by their own validation. The returned error is a
// failure to list src.
func PathCollisions(src discovery.Source, loader modulerunner.ModuleLoader, namespace string) ([]error, error) {
	infos, err := src.List()
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return nil, err
	}

	// Addresses of the tree: namespaces of all modules and all their prefixes.
	// A module address maps to true, a pure namespace directory to false.
	addrs := map[string]bool{}
	var moduleNs []string
	for _, info := range infos {
		if addrs[info.Name] {
			continue
		}
		moduleNs = append(moduleNs, info.Name)
		addrs[info.Name] = true
		segs := strings.Split(info.Name, ".")
		for i := 1; i < len(segs); i++ {
			prefix := strings.Join(segs[:i], ".")
			if _, ok := addrs[prefix]; !ok {
				addrs[prefix] = false
			}
		}
	}
	sort.Strings(moduleNs)

	var out []error
	for _, ns := range moduleNs {
		if namespace != "" && ns != namespace && !strings.HasPrefix(ns, namespace+".") {
			continue
		}
		manifest, root, err := src.Resolve(ns)
		if err != nil {
			continue
		}
		mod, err := modulerunner.LoadModule(loader, manifest, root)
		if err != nil {
			continue
		}
		for _, p := range commandPaths(nil, mod.Commands) {
			addr := ns + "." + strings.Join(p, ".")
			isModule, ok := addrs[addr]
			if !ok {
				continue
			}
			ce := &modulerunner.CollisionError{
				Path: append(strings.Split(ns, "."), p...),
				A:    modulerunner.DescribeCommand(ns, p, manifest),
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
	return out, nil
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
