package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/olekukonko/tablewriter"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/modules"
	"github.com/tivjnb/futi/internal/termtext"
)

// cliStreams wires the manager to the given streams. out receives progress
// output, errw diagnostics; stdin supplies answers to prompts. The Interactive
// check is a closure so tests can replace stdinIsTerminal.
func cliStreams(out, errw io.Writer, stdin io.Reader) modules.Streams {
	return modules.Streams{
		Prompt: modules.NewPrompter(stdin, func() bool { return stdinIsTerminal() }),
		Out:    out,
		Err:    errw,
		Stdin:  stdin,
	}
}

// newCLIManager builds a Manager writing to out and errw and reading answers from stdin.
func newCLIManager(out, errw io.Writer, stdin io.Reader, rt *core.Runtime) *modules.Manager {
	return modules.New(rt, cliStreams(out, errw, stdin))
}

// runModulesPath — `futi modules path <name>`: prints the module directory.
func runModulesPath(w, errw io.Writer, rt *core.Runtime, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: futi modules path <name>")
	}
	dir, err := newCLIManager(w, errw, nil, rt).Path(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(w, dir)
	return nil
}

// runModulesInfo — `futi modules info <name>`: prints the path and the
// __futi_meta.yaml contents. A module without a sidecar gets a note.
func runModulesInfo(w, errw io.Writer, rt *core.Runtime, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: futi modules info <name>")
	}
	info, err := newCLIManager(w, errw, nil, rt).Info(args[0])
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
func runModulesList(w, errw io.Writer, rt *core.Runtime, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: futi modules list")
	}
	entries, err := newCLIManager(w, errw, nil, rt).List()
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

// parseInitArgs разбирает аргументы подкоманды futi modules init.
// Позиционный аргумент — path (обязательный). Флаги:
// --global, --desc <value>, --default-cmd <value>, --command <name> (repeatable).
// Флаги, требующие значение (--desc, --default-cmd, --command), отклоняют
// значения, начинающиеся с -- (фикс #48).
func parseInitArgs(args []string) (*modules.InitOptions, error) {
	cfg := &modules.InitOptions{}
	var positional []string

	// requireValue извлекает значение для флага и проверяет, что оно не
	// начинается с -- (фикс #48 — --desc --global не должно захватывать --global).
	requireValue := func(flagName string, i int) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flagName)
		}
		val := args[i+1]
		if strings.HasPrefix(val, "--") {
			return "", fmt.Errorf("%s requires a value, got flag %s", flagName, val)
		}
		return val, nil
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--global":
			cfg.Global = true
		case arg == "--desc":
			val, err := requireValue("--desc", i)
			if err != nil {
				return nil, err
			}
			cfg.Description = val
			i++
		case strings.HasPrefix(arg, "--desc="):
			cfg.Description = strings.TrimPrefix(arg, "--desc=")
		case arg == "--default-cmd":
			val, err := requireValue("--default-cmd", i)
			if err != nil {
				return nil, err
			}
			cfg.DefaultCmd = val
			i++
		case strings.HasPrefix(arg, "--default-cmd="):
			cfg.DefaultCmd = strings.TrimPrefix(arg, "--default-cmd=")
		case arg == "--command":
			val, err := requireValue("--command", i)
			if err != nil {
				return nil, err
			}
			cfg.Commands = append(cfg.Commands, val)
			i++
		case strings.HasPrefix(arg, "--command="):
			cfg.Commands = append(cfg.Commands, strings.TrimPrefix(arg, "--command="))
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) == 0 {
		return nil, fmt.Errorf("usage: futi modules init <name> [flags]")
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one path argument, got %d", len(positional))
	}
	cfg.Path = positional[0]

	if err := modules.ValidateInitPath(cfg.Path); err != nil {
		return nil, err
	}

	if cfg.Description == "" {
		cfg.Description = "My tool"
	}

	return cfg, nil
}

// runModulesInit — `futi modules init`: parses args and creates the module.
func runModulesInit(w, errw io.Writer, args []string) error {
	opts, err := parseInitArgs(args)
	if err != nil {
		return err
	}
	return modules.Init(w, errw, *opts)
}

// parseAddCommandArgs разбирает аргументы: позиционные module, command;
// флаги --desc <text>, --cmd <shell> (паттерн requireValue).
func parseAddCommandArgs(args []string) (*modules.AddCommandOptions, error) {
	cfg := &modules.AddCommandOptions{}
	var positional []string

	requireValue := func(flagName string, i int) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flagName)
		}
		val := args[i+1]
		if strings.HasPrefix(val, "--") {
			return "", fmt.Errorf("%s requires a value, got flag %s", flagName, val)
		}
		return val, nil
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--desc":
			val, err := requireValue("--desc", i)
			if err != nil {
				return nil, err
			}
			cfg.Desc = val
			i++
		case strings.HasPrefix(arg, "--desc="):
			cfg.Desc = strings.TrimPrefix(arg, "--desc=")
		case arg == "--cmd":
			val, err := requireValue("--cmd", i)
			if err != nil {
				return nil, err
			}
			cfg.Cmd = val
			i++
		case strings.HasPrefix(arg, "--cmd="):
			cfg.Cmd = strings.TrimPrefix(arg, "--cmd=")
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) != 2 {
		return nil, fmt.Errorf("usage: futi modules add_command <module> <command> [--desc <text>] [--cmd <shell>]")
	}
	cfg.Module, cfg.Command = positional[0], positional[1]
	if err := modules.ValidateResolveName(cfg.Module); err != nil {
		return nil, err
	}
	return cfg, nil
}

// runModulesAddCommand — `futi modules add_command`: adds a command to the
// module's config.yaml.
func runModulesAddCommand(w, errw io.Writer, rt *core.Runtime, args []string) error {
	opts, err := parseAddCommandArgs(args)
	if err != nil {
		return err
	}
	return newCLIManager(w, errw, nil, rt).AddCommand(*opts)
}

// runModulesRemoveCommand — `futi modules remove_command`: removes a command
// from the module's config.yaml.
func runModulesRemoveCommand(w, errw io.Writer, rt *core.Runtime, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: futi modules remove_command <module> <command>")
	}
	if err := modules.ValidateResolveName(args[0]); err != nil {
		return err
	}
	return newCLIManager(w, errw, nil, rt).RemoveCommand(modules.RemoveCommandOptions{Module: args[0], Command: args[1]})
}

// parseUninstallArgs разбирает аргументы uninstall.
func parseUninstallArgs(args []string) (*modules.UninstallOptions, error) {
	cfg := &modules.UninstallOptions{}
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--force":
			cfg.Force = true
		case arg == "--yes" || arg == "-y":
			cfg.Yes = true
		case arg == "--skip-detach":
			cfg.SkipDetach = true
		case arg == "--strict-detach":
			cfg.StrictDetach = true
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) == 0 {
		return nil, fmt.Errorf("usage: futi modules uninstall <name> [flags]")
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one module name, got %d", len(positional))
	}
	cfg.Name = positional[0]
	if err := modules.ValidateResolveName(cfg.Name); err != nil {
		return nil, err
	}
	return cfg, nil
}

// runModulesUninstall — `futi modules uninstall`: parses args and removes the
// module. stdin supplies the confirmation answer.
func runModulesUninstall(w, errw io.Writer, stdin io.Reader, rt *core.Runtime, args []string) error {
	opts, err := parseUninstallArgs(args)
	if err != nil {
		return err
	}
	return newCLIManager(w, errw, stdin, rt).Uninstall(context.Background(), *opts)
}

// parseInstallArgs разбирает аргументы install (паттерн requireValue).
func parseInstallArgs(args []string) (*modules.InstallOptions, error) {
	cfg := &modules.InstallOptions{}
	var positional []string

	requireValue := func(flagName string, i int) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flagName)
		}
		val := args[i+1]
		if strings.HasPrefix(val, "--") {
			return "", fmt.Errorf("%s requires a value, got flag %s", flagName, val)
		}
		return val, nil
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--local":
			cfg.Local = true
		case arg == "--skip-attach":
			cfg.SkipAttach = true
		case arg == "--yes" || arg == "-y":
			cfg.Yes = true
		case arg == "--from-local":
			cfg.FromLocal = true
		case arg == "--from-git":
			cfg.FromGit = true
		case arg == "--from-archive":
			cfg.FromArchive = true
		case arg == "--namespace":
			val, err := requireValue("--namespace", i)
			if err != nil {
				return nil, err
			}
			cfg.Namespace = val
			i++
		case strings.HasPrefix(arg, "--namespace="):
			cfg.Namespace = strings.TrimPrefix(arg, "--namespace=")
		case arg == "--ref":
			val, err := requireValue("--ref", i)
			if err != nil {
				return nil, err
			}
			cfg.Ref = val
			i++
		case strings.HasPrefix(arg, "--ref="):
			cfg.Ref = strings.TrimPrefix(arg, "--ref=")
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) == 0 {
		return nil, fmt.Errorf("usage: futi modules install <source> [flags]")
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one source argument, got %d", len(positional))
	}
	cfg.Source = positional[0]
	if err := modules.ValidateInstallOptions(*cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// runModulesInstall — `futi modules install`: parses args and installs the
// module. stdin supplies the attach confirmation answer.
func runModulesInstall(w, errw io.Writer, stdin io.Reader, rt *core.Runtime, args []string) error {
	opts, err := parseInstallArgs(args)
	if err != nil {
		return err
	}
	return newCLIManager(w, errw, stdin, rt).Install(context.Background(), *opts)
}

// parseUpdateArgs разбирает аргументы update.
func parseUpdateArgs(args []string) (*modules.UpdateOptions, error) {
	cfg := &modules.UpdateOptions{}
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--skip-attach":
			cfg.SkipAttach = true
		case arg == "--yes" || arg == "-y":
			cfg.Yes = true
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) > 1 {
		return nil, fmt.Errorf("expected at most one module name, got %d", len(positional))
	}
	if len(positional) == 1 {
		cfg.Name = positional[0]
		if err := modules.ValidateResolveName(cfg.Name); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// runModulesUpdate — `futi modules update`: parses args and updates the named
// module or all installed ones. stdin answers replacement and attach prompts.
func runModulesUpdate(w, errw io.Writer, stdin io.Reader, rt *core.Runtime, args []string) error {
	opts, err := parseUpdateArgs(args)
	if err != nil {
		return err
	}
	return newCLIManager(w, errw, stdin, rt).Update(context.Background(), *opts)
}
