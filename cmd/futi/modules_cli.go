package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/modules"
	"github.com/tivjnb/futi/internal/termtext"
)

// cliStreams wires the manager to the process streams. w receives progress
// output; stdin supplies answers to prompts. The Interactive check is a
// closure so tests can replace stdinIsTerminal.
func cliStreams(w io.Writer, stdin io.Reader) modules.Streams {
	return modules.Streams{
		Prompt: modules.NewPrompter(stdin, func() bool { return stdinIsTerminal() }),
		Out:    w,
		Err:    os.Stderr,
		Stdin:  os.Stdin,
	}
}

// newCLIManager builds a Manager writing to w and reading answers from stdin.
func newCLIManager(w io.Writer, stdin io.Reader, rt *core.Runtime) *Manager {
	return New(rt, cliStreams(w, stdin))
}

// runModulesPath — `futi modules path <name>`: prints the module directory.
func runModulesPath(w io.Writer, rt *core.Runtime, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: futi modules path <name>")
	}
	dir, err := newCLIManager(w, nil, rt).Path(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(w, dir)
	return nil
}

// runModulesInfo — `futi modules info <name>`: prints the path and the
// __futi_meta.yaml contents. A module without a sidecar gets a note.
func runModulesInfo(w io.Writer, rt *core.Runtime, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: futi modules info <name>")
	}
	info, err := newCLIManager(w, nil, rt).Info(args[0])
	if info.Root != "" {
		fmt.Fprintf(w, "Path: %s\n", termtext.Line(info.Root))
	}
	if err != nil {
		return err
	}
	if info.Meta == nil {
		fmt.Fprintln(w, "Installed: no (no __futi_meta.yaml — hand-authored or created via modules init)")
		return nil
	}

	meta := info.Meta
	lf := meta.Lockfile
	fmt.Fprintf(w, "Source: %s\n", termtext.Line(lf.Source))
	fmt.Fprintf(w, "Type: %s\n", termtext.Line(lf.Type))
	if lf.Type == "git" && lf.Ref != "" {
		fmt.Fprintf(w, "Ref: %s\n", termtext.Line(lf.Ref))
	}
	if lf.Commit != "" {
		fmt.Fprintf(w, "Commit: %s\n", termtext.Line(lf.Commit))
	}
	if lf.Hash != "" {
		fmt.Fprintf(w, "Hash: %s\n", termtext.Line(lf.Hash))
	}
	if !lf.InstalledAt.IsZero() {
		fmt.Fprintf(w, "Installed at: %s\n", lf.InstalledAt.Format("2006-01-02 15:04:05"))
	}
	if !lf.LastCheck.IsZero() {
		fmt.Fprintf(w, "Last check: %s\n", lf.LastCheck.Format("2006-01-02 15:04:05"))
	}
	if meta.About != nil {
		if meta.About.Author != "" {
			fmt.Fprintf(w, "Author: %s\n", termtext.Line(meta.About.Author))
		}
		if meta.About.License != "" {
			fmt.Fprintf(w, "License: %s\n", termtext.Line(meta.About.License))
		}
		if meta.About.Homepage != "" {
			fmt.Fprintf(w, "Homepage: %s\n", termtext.Line(meta.About.Homepage))
		}
		if len(meta.About.Tags) > 0 {
			fmt.Fprintf(w, "Tags: %s\n", termtext.Line(strings.Join(meta.About.Tags, ", ")))
		}
	}
	return nil
}

// runModulesList — `futi modules list`: prints the installed modules.
func runModulesList(w io.Writer, rt *core.Runtime, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: futi modules list")
	}
	entries, err := newCLIManager(w, nil, rt).List()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(w, "No installed modules found.")
		return nil
	}

	var rows [][]string
	for _, e := range entries {
		// Ref осмыслен только для git; в старых lockfile local/archive он мог остаться.
		version := ""
		if e.Meta.Lockfile.Type == "git" {
			version = e.Meta.Lockfile.Ref
		}
		if version == "" {
			version = e.Meta.Lockfile.Commit
			if len(version) > 12 {
				version = version[:12]
			}
		}
		installed := ""
		if !e.Meta.Lockfile.InstalledAt.IsZero() {
			installed = e.Meta.Lockfile.InstalledAt.Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{termtext.Line(e.Name), termtext.Line(e.Meta.Lockfile.Source), termtext.Line(version), installed})
	}

	tw := tablewriter.NewTable(w)
	tw.Header("NAME", "SOURCE", "VERSION", "INSTALLED_AT")
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("modules list: render: %w", err)
	}
	return tw.Render()
}
