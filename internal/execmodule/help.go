package execmodule

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/inputs"
	"github.com/tivjnb/futi/internal/termtext"
)

// FormatCommandHelp formats the help of a node (the module itself or a
// command): description, usage, args, flags and subcommands. cmdPath is the
// full path for usage, for example "docker build". Pure function, no I/O.
func FormatCommandHelp(cmdPath string, cmd *Command) string {
	var b strings.Builder
	path := termtext.Line(cmdPath)

	if cmd.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", termtext.Block(cmd.Description))
	}

	runnable := !cmd.Cmd.IsZero()
	b.WriteString("usage: futi " + path)
	switch {
	case runnable:
		if len(cmd.Flags) > 0 {
			b.WriteString(" [flags]")
		}
		for _, a := range cmd.Args {
			b.WriteString(" " + argUsage(a))
		}
	default:
		b.WriteString(" <subcommand>")
	}
	b.WriteString("\n")

	if runnable {
		writeArgs(&b, cmd.Args)
		writeFlags(&b, cmd.Flags)
	}

	if len(cmd.Commands) > 0 {
		b.WriteString("subcommands:\n")
		names := sortedNames(cmd.Commands)
		width := 0
		for _, name := range names {
			width = max(width, utf8.RuneCountInString(termtext.Line(name)))
		}
		for _, name := range names {
			clean := termtext.Line(name)
			line := "  " + clean
			if d := cmd.Commands[name].Description; d != "" {
				line += strings.Repeat(" ", width-utf8.RuneCountInString(clean)) + "  " + termtext.Line(d)
			}
			b.WriteString(line + "\n")
		}
		if w, ok := shadowWarning(cmdPath, cmd); ok {
			b.WriteString("warning: " + termtext.Line(w) + "\n")
		}
	}

	return b.String()
}

// argUsage is the token of an arg in the usage line: <name> for a required
// arg, [name] for an optional one, with "..." for a variadic or rest arg.
func argUsage(a inputs.Spec) string {
	name := termtext.Line(a.Name)
	if a.Variadic || a.Rest {
		if a.Required {
			return "<" + name + ">..."
		}
		return "[" + name + "...]"
	}
	if a.Required {
		return "<" + name + ">"
	}
	return "[" + name + "]"
}

// writeArgs writes the args block: one line per arg with its description and
// (required), (default "x") or (passed verbatim).
func writeArgs(b *strings.Builder, args []inputs.Spec) {
	rows := make([]row, len(args))
	for i, a := range args {
		name := termtext.Line(a.Name)
		if a.Variadic || a.Rest {
			name += "..."
		}
		var meta []string
		if a.Required {
			meta = append(meta, "(required)")
		}
		if a.HasDefault {
			meta = append(meta, defaultMeta(a))
		}
		if a.Rest {
			meta = append(meta, "(passed verbatim)")
		}
		rows[i] = row{sig: name, desc: termtext.Line(a.Description), meta: meta}
	}
	writeRows(b, rows)
}

// writeFlags writes the flags: block, ending with -h, --help.
func writeFlags(b *strings.Builder, flags []inputs.Spec) {
	b.WriteString("flags:\n")
	rows := make([]row, 0, len(flags)+1)
	for _, f := range flags {
		sig := "    "
		if f.Short != "" {
			sig = "-" + termtext.Line(f.Short) + ", "
		}
		sig += "--" + strings.ReplaceAll(termtext.Line(f.Name), "_", "-")
		if f.Kind() != argvtmpl.Bool {
			typ := f.Type
			if typ == "" {
				typ = "string"
			}
			sig += " " + termtext.Line(typ)
			if f.Variadic {
				sig += "..."
			}
		}
		var meta []string
		if f.Required {
			meta = append(meta, "(required)")
		}
		if f.HasDefault {
			meta = append(meta, defaultMeta(f))
		}
		rows = append(rows, row{sig: sig, desc: termtext.Line(f.Description), meta: meta})
	}
	rows = append(rows, row{sig: "-h, --help"})
	writeRows(b, rows)
}

// defaultMeta is "(default "x")" for a string input and "(default 100)" for
// the others.
func defaultMeta(s inputs.Spec) string {
	val := termtext.Line(s.Default)
	if s.Type == "" || s.Type == "string" {
		return fmt.Sprintf("(default %q)", val)
	}
	return "(default " + val + ")"
}

// row is one line of an args or flags block.
type row struct {
	sig  string
	desc string
	meta []string
}

// writeRows writes rows with the descriptions aligned in a column.
func writeRows(b *strings.Builder, rows []row) {
	width := 0
	for _, r := range rows {
		width = max(width, utf8.RuneCountInString(r.sig))
	}
	for _, r := range rows {
		rest := strings.TrimSpace(r.desc + " " + strings.Join(r.meta, " "))
		line := "  " + r.sig
		if rest != "" {
			line += strings.Repeat(" ", width-utf8.RuneCountInString(r.sig)) + "  " + rest
		}
		b.WriteString(line + "\n")
	}
}
