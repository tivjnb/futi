package execmodule

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/termtext"
)

// FormatCommandHelp формирует справку по команде (листовой, с Cmd):
// описание, usage, параметры (имя/тип/required/default/description),
// позиционные требования и итоговую cmd-строку. cmdPath — полный путь для
// usage, например "demo greet". Чистая функция, без I/O.
func FormatCommandHelp(cmdPath string, cmd *Command) string {
	var b strings.Builder

	if cmd.Description != "" {
		fmt.Fprintf(&b, "%s — %s\n\n", termtext.Line(cmdPath), termtext.Block(cmd.Description))
	} else {
		fmt.Fprintf(&b, "%s\n\n", termtext.Line(cmdPath))
	}

	// USAGE: futi <cmdPath> [--params] [-- args]
	b.WriteString("USAGE\n")
	fmt.Fprintf(&b, "  futi %s", termtext.Line(cmdPath))
	for _, p := range cmd.Params {
		if p.Type == "bool" {
			fmt.Fprintf(&b, " [--%s]", termtext.Line(p.Name))
		} else {
			fmt.Fprintf(&b, " [--%s value]", termtext.Line(p.Name))
		}
	}
	if cmd.Cmd != "" {
		b.WriteString(" [-- args]")
	}
	b.WriteString("\n")

	// PARAMETERS
	if len(cmd.Params) > 0 {
		b.WriteString("\nPARAMETERS\n")
		// Выравнивание по ширине самого длинного "--name".
		max := 0
		for _, p := range cmd.Params {
			if l := len("--" + termtext.Line(p.Name)); l > max {
				max = l
			}
		}
		for _, p := range cmd.Params {
			name := termtext.Line(p.Name)
			pad := strings.Repeat(" ", max-len("--"+name))
			meta := paramMeta(p)
			line := fmt.Sprintf("  --%s%s  %s", name, pad, meta)
			if p.Description != "" {
				line += "  " + termtext.Line(p.Description)
			}
			b.WriteString(line + "\n")
		}
	}

	// POSITIONAL
	if cmd.Positional.Min > 0 {
		noun := "argument"
		if cmd.Positional.Min != 1 {
			noun = "arguments"
		}
		b.WriteString("\nPOSITIONAL\n")
		fmt.Fprintf(&b, "  at least %d %s after --\n", cmd.Positional.Min, noun)
	}

	// CMD
	if cmd.Cmd != "" {
		b.WriteString("\nCMD\n")
		fmt.Fprintf(&b, "  %s\n", termtext.Block(cmd.Cmd))
	}

	return b.String()
}

// paramMeta возвращает "(type[, required|default])" для параметра.
func paramMeta(p Param) string {
	typ := termtext.Line(p.Type)
	if typ == "" {
		typ = "string"
	}
	switch {
	case p.Required:
		return fmt.Sprintf("(%s, required)", typ)
	case p.Default != "":
		return fmt.Sprintf("(%s, default: %s)", typ, termtext.Line(p.Default))
	default:
		return fmt.Sprintf("(%s)", typ)
	}
}

// FormatModuleHelp формирует справку по модулю (или ветке-команде с subcommands):
// описание, default cmd, список команд с описаниями. ns — полный namespace/путь.
// Чистая функция, без I/O.
func FormatModuleHelp(mod *Module, ns string) string {
	var b strings.Builder

	if mod.Description != "" {
		fmt.Fprintf(&b, "%s — %s\n", termtext.Line(ns), termtext.Block(mod.Description))
	} else {
		fmt.Fprintf(&b, "%s\n", termtext.Line(ns))
	}

	if mod.Cmd != "" {
		fmt.Fprintf(&b, "default: %s\n", termtext.Block(mod.Cmd))
	}

	// Схема default-команды: параметры и позиционные аргументы.
	if len(mod.Params) > 0 {
		b.WriteString("default params:\n")
		max := 0
		for _, p := range mod.Params {
			if l := len("--" + termtext.Line(p.Name)); l > max {
				max = l
			}
		}
		for _, p := range mod.Params {
			name := termtext.Line(p.Name)
			pad := strings.Repeat(" ", max-len("--"+name))
			line := fmt.Sprintf("  --%s%s  %s", name, pad, paramMeta(p))
			if p.Description != "" {
				line += "  " + termtext.Line(p.Description)
			}
			b.WriteString(line + "\n")
		}
	}
	if mod.Positional.Min > 0 {
		noun := "argument"
		if mod.Positional.Min != 1 {
			noun = "arguments"
		}
		fmt.Fprintf(&b, "default positional: at least %d %s after --\n", mod.Positional.Min, noun)
	}

	if len(mod.Commands) > 0 {
		b.WriteString("commands:\n")
		names := make([]string, 0, len(mod.Commands))
		for name := range mod.Commands {
			names = append(names, name)
		}
		sort.Strings(names)
		max := 0
		for _, name := range names {
			if l := len(termtext.Line(name)); l > max {
				max = l
			}
		}
		for _, name := range names {
			cmd := mod.Commands[name]
			clean := termtext.Line(name)
			pad := strings.Repeat(" ", max-len(clean))
			line := fmt.Sprintf("  %s%s", clean, pad)
			if cmd.Description != "" {
				line += "  " + termtext.Line(cmd.Description)
			}
			b.WriteString(line + "\n")
		}
	}

	return b.String()
}
