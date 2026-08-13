package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"futi/internal/discovery"
	"futi/internal/execmodule"
	"futi/internal/modulerunner"
)

var (
	errListCommand = errors.New("list command requested")
	errHelp        = errors.New("help requested")
)

// listRequest сигнализирует, что запрошен листинг namespace или модуля.
type listRequest struct {
	namespace string
	verbose   bool
	recurse   bool
}

func (e *listRequest) Error() string { return "list command requested" }
func (e *listRequest) Unwrap() error { return errListCommand }

// parseArgs разбирает позиционные аргументы и флаги.
// Правило: позиционные аргументы (без --) = namespace... + command
// (последний = команда, остальное = namespace через ".").
// Флаги: --param val, --param=val, --bool-flag.
// "list" как последний аргумент — запрос листинга.
func parseArgs(args []string) (namespace, command string, params map[string]string, err error) {
	params = make(map[string]string)
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]

		if arg == "-h" || arg == "--help" {
			return "", "", nil, errHelp
		}

		if arg == "-v" || arg == "--verbose" {
			params["verbose"] = "true"
			i++
			continue
		}

		if arg == "-r" || arg == "--recurse" {
			params["recurse"] = "true"
			i++
			continue
		}

		if strings.HasPrefix(arg, "--") {
			name := strings.TrimPrefix(arg, "--")
			if idx := strings.Index(name, "="); idx >= 0 {
				params[name[:idx]] = name[idx+1:]
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				params[name] = args[i+1]
				i++
			} else {
				params[name] = "true"
			}
		} else {
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) == 0 {
		return "", "", nil, errors.New("usage: futi <namespace...> <command> [--param val]")
	}

	if positional[len(positional)-1] == "list" {
		ns := strings.Join(positional[:len(positional)-1], ".")
		return "", "", nil, &listRequest{
			namespace: ns,
			verbose:   params["verbose"] == "true",
			recurse:   params["recurse"] == "true",
		}
	}

	if len(positional) < 2 {
		return "", "", nil, fmt.Errorf("usage: futi <namespace...> <command> [--param val], got: %v", positional)
	}

	command = positional[len(positional)-1]
	namespace = strings.Join(positional[:len(positional)-1], ".")

	return namespace, command, params, nil
}

func modulesDir() string {
	if dir := os.Getenv("FUTI_MODULES_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory: %v", err)
	}
	return filepath.Join(home, ".config", "futi", "modules")
}

// runList выводит содержимое namespace или команды модуля.
// Если namespace резолвится как модуль — показывает команды (-r игнорируется).
// Иначе — показывает непосредственных детей namespace, или всё поддерево при recurse.
// При verbose=true добавляет колонку PATH.
func runList(namespace string, verbose, recurse bool) error {
	source := discovery.NewGlobalSource(modulesDir())

	// Сначала пробуем резолвить как модуль
	manifestPath, moduleRoot, err := source.Resolve(namespace)
	if err == nil {
		return printCommands(manifestPath, moduleRoot, verbose)
	}

	// Не модуль — пробуем как namespace
	var children []discovery.Child
	var listErr error
	if recurse {
		children, listErr = source.ListChildrenRecursive(namespace)
	} else {
		children, listErr = source.ListChildren(namespace)
	}
	if listErr != nil {
		return listErr
	}
	if len(children) == 0 {
		fmt.Println("No modules found.")
		return nil
	}
	return printChildren(children, verbose)
}

func printCommands(manifestPath, moduleRoot string, verbose bool) error {
	mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}

	if len(mod.Commands) == 0 {
		fmt.Println("No commands found.")
		return nil
	}

	if verbose {
		fmt.Printf("%-20s %-40s %s\n", "COMMAND", "DESCRIPTION", "PATH")
		for name, cmd := range mod.Commands {
			fmt.Printf("%-20s %-40s %s\n", name, cmd.Description, manifestPath)
		}
	} else {
		fmt.Printf("%-20s %s\n", "COMMAND", "DESCRIPTION")
		for name, cmd := range mod.Commands {
			fmt.Printf("%-20s %s\n", name, cmd.Description)
		}
	}
	return nil
}

func printChildren(children []discovery.Child, verbose bool) error {
	type row struct {
		typeStr     string
		name        string
		description string
		path        string
	}
	rows := make([]row, 0, len(children))
	for _, c := range children {
		var desc, path string
		if c.Type == "module" {
			path = c.ManifestPath
			if mod, err := execmodule.LoadModule(c.ManifestPath, c.ModuleRoot); err == nil {
				desc = mod.Description
			}
		}
		rows = append(rows, row{c.Type, c.Name, desc, path})
	}

	if verbose {
		fmt.Printf("%-12s %-20s %-30s %s\n", "TYPE", "NAME", "DESCRIPTION", "PATH")
		for _, r := range rows {
			fmt.Printf("%-12s %-20s %-30s %s\n", r.typeStr, r.name, r.description, r.path)
		}
	} else {
		fmt.Printf("%-12s %-20s %s\n", "TYPE", "NAME", "DESCRIPTION")
		for _, r := range rows {
			fmt.Printf("%-12s %-20s %s\n", r.typeStr, r.name, r.description)
		}
	}
	return nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: futi <namespace...> <command> [--param val]")
	fmt.Fprintln(os.Stderr, "       futi <namespace...> list")
	fmt.Fprintln(os.Stderr, "       futi list")
	fmt.Fprintln(os.Stderr, "       futi -h | --help")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	namespace, command, params, err := parseArgs(os.Args[1:])
	if err != nil {
		var req *listRequest
		switch {
		case errors.As(err, &req):
			if listErr := runList(req.namespace, req.verbose, req.recurse); listErr != nil {
				log.Fatalf("list: %v", listErr)
			}
			return
		case errors.Is(err, errListCommand):
			if listErr := runList("", false, false); listErr != nil {
				log.Fatalf("list: %v", listErr)
			}
			return
		case errors.Is(err, errHelp):
			printUsage()
			return
		default:
			log.Fatalf("parse: %v", err)
		}
	}

	source := discovery.NewGlobalSource(modulesDir())
	if err := modulerunner.Run(context.Background(), source, namespace, command, params, nil, nil); err != nil {
		log.Fatalf("run: %v", err)
	}
}
