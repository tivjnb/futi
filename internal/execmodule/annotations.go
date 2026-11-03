package execmodule

import (
	"fmt"
	"slices"
)

// Frontend is a way to reach a command: the CLI, an MCP server or an HTTP API.
type Frontend string

const (
	FrontendCLI  Frontend = "cli"
	FrontendMCP  Frontend = "mcp"
	FrontendHTTP Frontend = "http"
)

// AllFrontends lists every frontend in the canonical order.
var AllFrontends = []Frontend{FrontendCLI, FrontendMCP, FrontendHTTP}

// Tri is a boolean annotation that may be unset. The zero value is "not
// set": gob drops zero values, so a *bool holding false would come back from
// the module cache as nil.
type Tri uint8

const (
	TriUnset Tri = iota
	TriFalse
	TriTrue
)

// IsTrue reports whether the annotation is explicitly true.
func (t Tri) IsTrue() bool { return t == TriTrue }

func triOf(b *bool) Tri {
	switch {
	case b == nil:
		return TriUnset
	case *b:
		return TriTrue
	default:
		return TriFalse
	}
}

// ExposedTo reports whether the command is reachable from the frontend. An
// empty Expose (a zero Command) is visible everywhere.
func (c *Command) ExposedTo(f Frontend) bool {
	return len(c.Expose) == 0 || slices.Contains(c.Expose, f)
}

// IsDestructive reports whether the command is explicitly marked destructive.
func (c *Command) IsDestructive() bool { return c.Destructive == TriTrue }

// parseExpose computes the effective expose list of a node: the parent's list
// when the key is absent, otherwise the listed values in AllFrontends order.
// Schema problems go to errs.
func parseExpose(raw *[]string, path string, parent []Frontend, errs *[]error) []Frontend {
	if raw == nil {
		return parent
	}
	if len(*raw) == 0 {
		*errs = append(*errs, fmt.Errorf("command %q: expose is empty: the command is unreachable", path))
		return parent // keep children free of follow-up errors
	}
	var seen []Frontend
	for _, s := range *raw {
		f := Frontend(s)
		switch {
		case !slices.Contains(AllFrontends, f):
			*errs = append(*errs, fmt.Errorf("command %q: unknown expose value %q (want cli, mcp or http)", path, s))
		case slices.Contains(seen, f):
			*errs = append(*errs, fmt.Errorf("command %q: duplicate expose value %q", path, s))
		case !slices.Contains(parent, f):
			*errs = append(*errs, fmt.Errorf("command %q: expose widens the parent: %q is not exposed by %q", path, s, parentPath(path)))
		default:
			seen = append(seen, f)
		}
	}
	var out []Frontend
	for _, f := range AllFrontends {
		if slices.Contains(seen, f) {
			out = append(out, f)
		}
	}
	return out
}

// parentPath is the path of the parent of the command at path.
func parentPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[:i]
		}
	}
	return defaultPath
}

// checkAnnotations reports annotations on a node without its own cmd and the
// readonly/destructive conflict.
func checkAnnotations(path string, cmd *Command, errors *[]error) {
	if cmd.Cmd.IsZero() {
		for _, a := range []struct {
			key string
			val Tri
		}{{"readonly", cmd.ReadOnly}, {"destructive", cmd.Destructive}, {"idempotent", cmd.Idempotent}} {
			if a.val != TriUnset {
				*errors = append(*errors, fmt.Errorf("command %q: %s applies only to a command with cmd", path, a.key))
			}
		}
	}
	if cmd.ReadOnly == TriTrue && cmd.Destructive == TriTrue {
		*errors = append(*errors, fmt.Errorf("command %q: readonly and destructive are mutually exclusive", path))
	}
}
