package main

import (
	"fmt"
	"io"

	"futi/internal/discovery"
	"futi/internal/modulecache"
)

// runModules — диспетчер `futi modules <subcommand> [...]`.
// source — источник модулей (resolve для path/info/add_command/remove_command/
// uninstall/update); cache — кэш (Invalidate после install/uninstall/update).
// args — аргументы после "modules" (os.Args[2:]).
// stdin — для интерактивного подтверждения uninstall (os.Stdin в main).
func runModules(w io.Writer, stdin io.Reader, source discovery.Source, cache *modulecache.Cache, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: futi modules <subcommand> (subcommands: init, install, uninstall, update, info, list, add_command, remove_command, path)")
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return runModulesInit(w, rest)
	case "install":
		return runModulesInstall(w, cache, rest)
	case "uninstall":
		return runModulesUninstall(w, stdin, source, cache, rest)
	case "update":
		return runModulesUpdate(w, source, cache, rest)
	case "info":
		return runModulesInfo(w, source, rest)
	case "list":
		return runModulesList(w, rest)
	case "add_command":
		return runModulesAddCommand(w, source, rest)
	case "remove_command":
		return runModulesRemoveCommand(w, source, rest)
	case "path":
		return runModulesPath(w, source, rest)
	default:
		return fmt.Errorf("unknown modules subcommand %q", sub)
	}
}


