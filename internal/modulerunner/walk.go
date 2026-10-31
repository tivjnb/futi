package modulerunner

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

// Node is a node of the merged command tree: the root, a namespace directory,
// a module or a command inside a module.
type Node struct {
	Path         []string           // matched segments (dotted words split)
	Namespace    string             // discovery namespace of the node: Path joined by "."
	Module       *execmodule.Module // nil for a namespace node (root or directory)
	ManifestPath string
	ModuleRoot   string
	// Command is the command of the node inside a module; on the module level
	// it is the module's own command (cmd, params, commands).
	Command *execmodule.Command
	CmdPath []string // command path inside Module; empty on the module level
}

// WalkOptions configures Walk.
type WalkOptions struct {
	// Verified loads modules with LoadModuleVerified (execution); otherwise
	// with LoadModule (help, list, completion).
	Verified bool
}

// CollisionError reports a path that addresses two entities of the merged
// tree, e.g. a module command and a module (V4).
type CollisionError struct {
	Path []string
	A, B string // entity descriptions, see DescribeCommand, DescribeModule, DescribeNamespace
}

func (e *CollisionError) Error() string {
	return fmt.Sprintf("path %q is ambiguous: %s and %s", strings.Join(e.Path, " "), e.A, e.B)
}

// DescribeCommand describes command cmdPath of the module at namespace module
// for a CollisionError.
func DescribeCommand(module string, cmdPath []string, manifest string) string {
	return fmt.Sprintf("command %q of module %s (%s)", strings.Join(cmdPath, " "), module, manifest)
}

// DescribeModule describes the module at namespace ns for a CollisionError.
func DescribeModule(ns, manifest string) string {
	return fmt.Sprintf("module %s (%s)", ns, manifest)
}

// DescribeNamespace describes namespace directory ns for a CollisionError.
func DescribeNamespace(ns string) string {
	return "namespace " + ns
}

// PathError is a command path that does not lead to a runnable command:
// an unknown word or a node without cmd. It matches its class (ErrModuleNotFound,
// execmodule.ErrCommandNotFound or execmodule.ErrRequiresSubcommand) by errors.Is.
type PathError struct {
	Path  []string // path of the node the error is about
	Word  string   // the unknown word; empty for a missing subcommand
	msg   string
	class error
}

func (e *PathError) Error() string        { return e.msg }
func (e *PathError) Is(target error) bool { return target == e.class }

// Walk reads words left to right along the merged tree of source: namespace
// directories, modules and their commands. A dotted word continues the path
// only if all its segments match. The walk stops at "--", at a token starting
// with "-" (longer than one character), at the first unmatched word or at the
// end. It returns the reached node and the number of consumed words. A segment
// that is both a module command and a discovery child is a *CollisionError;
// source and module load errors are returned as is.
func Walk(source discovery.Source, loader ModuleLoader, words []string, opt WalkOptions) (*Node, int, error) {
	node := &Node{}
	n := 0
	for _, w := range words {
		if w == "--" || isFlagToken(w) {
			break
		}
		next := node
		for _, seg := range strings.Split(w, ".") {
			var err error
			next, err = next.step(source, loader, seg, opt)
			if err != nil {
				return nil, n, err
			}
			if next == nil {
				break
			}
		}
		if next == nil {
			break
		}
		node = next
		n++
	}
	return node, n, nil
}

// step returns the child seg of n, or nil if there is none.
func (n *Node) step(source discovery.Source, loader ModuleLoader, seg string, opt WalkOptions) (*Node, error) {
	if seg == "" {
		return nil, nil
	}
	var sub *execmodule.Command
	if n.Command != nil {
		sub = n.Command.Commands[seg]
	}
	ns := seg
	if n.Namespace != "" {
		ns = n.Namespace + "." + seg
	}
	path := append(append([]string(nil), n.Path...), seg)

	// A listing with unreadable sources is incomplete: the result of a
	// *PartialError is used, other errors give no children.
	children, err := source.ListChildren(n.Namespace)
	incomplete := false
	if err != nil && !errors.Is(err, discovery.ErrNotFound) {
		var partial *discovery.PartialError
		if !errors.As(err, &partial) {
			children = nil
		}
		incomplete = true
	}
	var isModule, isNamespace bool
	for _, c := range children {
		if c.Name == seg {
			isModule = isModule || c.Type == "module"
			isNamespace = isNamespace || c.Type == "namespace"
		}
	}

	if sub != nil {
		if isModule || isNamespace {
			return nil, n.collision(source, path, ns, isModule)
		}
		return &Node{
			Path: path, Namespace: ns,
			Module: n.Module, ManifestPath: n.ManifestPath, ModuleRoot: n.ModuleRoot,
			Command: sub, CmdPath: append(append([]string(nil), n.CmdPath...), seg),
		}, nil
	}

	// An incomplete listing may miss the child: ask Resolve directly, so that
	// its read error is reported instead of an unknown command. A word after
	// a runnable node may be a value: no probe there.
	probe := incomplete && !isModule && !isNamespace && !n.runnable()
	if !isModule && !probe {
		if isNamespace {
			return &Node{Path: path, Namespace: ns}, nil
		}
		return nil, nil
	}
	manifest, root, err := source.Resolve(ns)
	if err != nil {
		if probe && errors.Is(err, discovery.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	load := LoadModule
	if opt.Verified {
		load = LoadModuleVerified
	}
	mod, err := load(loader, manifest, root)
	if err != nil {
		return nil, err
	}
	return &Node{
		Path: path, Namespace: ns,
		Module: mod, ManifestPath: manifest, ModuleRoot: root,
		Command: moduleCommand(mod),
	}, nil
}

// runnable reports whether the node has its own cmd.
func (n *Node) runnable() bool {
	return n.Command != nil && n.Command.Cmd != ""
}

// collision builds the error for segment path[len-1], which is a command of
// n's module and a discovery child at ns (a module if isModule).
func (n *Node) collision(source discovery.Source, path []string, ns string, isModule bool) error {
	moduleNs := strings.Join(n.Path[:len(n.Path)-len(n.CmdPath)], ".")
	cmdPath := append(append([]string(nil), n.CmdPath...), path[len(path)-1])
	ce := &CollisionError{Path: path, A: DescribeCommand(moduleNs, cmdPath, n.ManifestPath), B: DescribeNamespace(ns)}
	if isModule {
		manifest, _, err := source.Resolve(ns)
		if err != nil {
			return err
		}
		ce.B = DescribeModule(ns, manifest)
	}
	return ce
}

// moduleCommand is the module's own command: its cmd, schema and commands.
func moduleCommand(mod *execmodule.Module) *execmodule.Command {
	return &execmodule.Command{
		Description: mod.Description,
		Params:      mod.Params,
		Positional:  mod.Positional,
		Cmd:         mod.Cmd,
		Commands:    mod.Commands,
	}
}

// Children returns the sorted names of the node's children: discovery children
// of its namespace and subcommands. Unreadable sources are skipped.
func (n *Node) Children(source discovery.Source) []string {
	seen := map[string]bool{}
	if n.Command != nil {
		for name := range n.Command.Commands {
			seen[name] = true
		}
	}
	children, err := source.ListChildren(n.Namespace)
	var partial *discovery.PartialError
	if err == nil || errors.As(err, &partial) {
		for _, c := range children {
			seen[c.Name] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveWords walks words with verified module loading and returns the
// runnable command of the reached node and the words left after the path.
// A node without cmd is a *PathError.
func ResolveWords(source discovery.Source, loader ModuleLoader, words []string) (*Resolved, []string, error) {
	node, n, err := Walk(source, loader, words, WalkOptions{Verified: true})
	if err != nil {
		var collision *CollisionError
		if errors.As(err, &collision) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("modulerunner: %w", err)
	}
	rest := words[n:]
	if !node.runnable() {
		return nil, nil, noCommandError(source, node, rest)
	}
	return &Resolved{
		Command:      node.Command,
		Module:       node.Module,
		ModuleRoot:   node.ModuleRoot,
		ManifestPath: node.ManifestPath,
		label:        strings.Join(node.Path, " "),
	}, rest, nil
}

// noCommandError explains why node, followed by rest, cannot run: an unknown
// word or a missing subcommand, with a hint when rest names a child of node.
func noCommandError(source discovery.Source, node *Node, rest []string) error {
	children := node.Children(source)
	var err *PathError
	if len(rest) > 0 && rest[0] != "--" && !isFlagToken(rest[0]) {
		err = UnknownCommandError(source, node, rest[0]).(*PathError)
	} else {
		err = &PathError{
			Path:  node.Path,
			msg:   fmt.Sprintf("%q requires a subcommand%s", nodeName(node), available(children)),
			class: execmodule.ErrRequiresSubcommand,
		}
	}
	for _, w := range rest {
		if w != "--" && slices.Contains(children, w) {
			err.msg += " (put the full command path before flags)"
			break
		}
	}
	return err
}

// UnknownCommandError reports word as an unknown child of node. Under the root
// it matches ErrModuleNotFound, elsewhere execmodule.ErrCommandNotFound.
func UnknownCommandError(source discovery.Source, node *Node, word string) error {
	if len(node.Path) == 0 {
		return &PathError{Word: word, msg: fmt.Sprintf("unknown command %q for \"futi\"", word), class: ErrModuleNotFound}
	}
	return &PathError{
		Path:  node.Path,
		Word:  word,
		msg:   fmt.Sprintf("unknown command %q for %q%s", word, nodeName(node), available(node.Children(source))),
		class: execmodule.ErrCommandNotFound,
	}
}

// nodeName is the command line of node: "futi docker build".
func nodeName(node *Node) string {
	return strings.Join(append([]string{"futi"}, node.Path...), " ")
}

func available(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return " (available: " + strings.Join(names, ", ") + ")"
}

// isFlagToken reports a token starting with "-" and longer than one character.
func isFlagToken(tok string) bool {
	return len(tok) > 1 && tok[0] == '-'
}
