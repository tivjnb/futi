package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
)

// stdinIsTerminal сообщает, подключён ли stdin к терминалу; шов для тестов.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runModules — диспетчер `futi modules <subcommand> [...]`.
// source — источник модулей (resolve для path/info/add_command/remove_command/
// uninstall/update); cache — кэш (Invalidate после install/uninstall/update).
// args — аргументы после "modules" (os.Args[2:]).
// stdin — для интерактивных подтверждений install/update/uninstall (os.Stdin в main).
func runModules(w io.Writer, stdin io.Reader, source discovery.Source, cache *modulecache.Cache, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: futi modules <subcommand> (subcommands: init, install, uninstall, update, info, validate, list, add_command, remove_command, path)")
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return runModulesInit(w, rest)
	case "install":
		return runModulesInstall(w, stdin, cache, rest)
	case "uninstall":
		return runModulesUninstall(w, stdin, source, cache, rest)
	case "update":
		return runModulesUpdate(w, stdin, source, cache, rest)
	case "info":
		return runModulesInfo(w, source, rest)
	case "validate":
		return runModulesValidate(w, source, rest)
	case "list":
		return runModulesList(w, rest)
	case "add_command":
		return runModulesAddCommand(w, source, cache, rest)
	case "remove_command":
		return runModulesRemoveCommand(w, source, cache, rest)
	case "path":
		return runModulesPath(w, source, rest)
	default:
		return fmt.Errorf("unknown modules subcommand %q", sub)
	}
}

// runModulesValidate — `futi modules validate <namespace>`: отчёт валидации
// схемы модуля (см. runValidate).
func runModulesValidate(w io.Writer, source discovery.Source, args []string) error {
	if len(args) != 1 || isFlagToken(args[0]) {
		return fmt.Errorf("usage: futi modules validate <namespace>")
	}
	if err := validateResolveName(args[0]); err != nil {
		return err
	}
	return runValidate(w, source, args[0])
}
