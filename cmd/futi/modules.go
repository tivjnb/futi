package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
)

// stdinIsTerminal сообщает, подключён ли stdin к терминалу; шов для тестов.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runModules — диспетчер `futi modules <subcommand> [...]`.
// rt — Runtime: источник модулей (resolve для path/info/add_command/
// remove_command/uninstall/update) и кэш (Invalidate после install/uninstall/
// update) берутся из него внутри ветки подкоманды; init, usage и unknown
// работают с rt == nil.
// args — аргументы после "modules" (os.Args[2:]).
// stdin — для интерактивных подтверждений install/update/uninstall (os.Stdin в main).
func runModules(w io.Writer, stdin io.Reader, rt *core.Runtime, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: futi modules <subcommand> (subcommands: init, install, uninstall, update, info, validate, list, add_command, remove_command, path)")
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return runModulesInit(w, rest)
	case "install":
		return runModulesInstall(w, stdin, rt, rest)
	case "uninstall":
		return runModulesUninstall(w, stdin, rt, rest)
	case "update":
		return runModulesUpdate(w, stdin, rt, rest)
	case "info":
		return runModulesInfo(w, rt, rest)
	case "validate":
		source, err := rt.Source()
		if err != nil {
			return err
		}
		return runModulesValidate(w, source, rest)
	case "list":
		return runModulesList(w, rt, rest)
	case "add_command":
		return runModulesAddCommand(w, rt, rest)
	case "remove_command":
		return runModulesRemoveCommand(w, rt, rest)
	case "path":
		return runModulesPath(w, rt, rest)
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
