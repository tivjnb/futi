package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"futi/internal/config"
	"futi/internal/discovery"
	"futi/internal/execmodule"
	"futi/internal/modulerunner"

	"github.com/olekukonko/tablewriter"
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

// createSource создаёт источник модулей.
// Если есть config.yaml — загружает конфиг и создаёт CompositeSource.
// Иначе — fallback на GlobalSource с ~/.config/futi/modules/ (или FUTI_MODULES_DIR).
func createSource() discovery.Source {
	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		log.Fatalf("config path: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if cfg != nil && len(cfg.Sources) > 0 {
		return discovery.NewCompositeSource(cfg)
	}

	// Fallback: нет конфига — одиночный GlobalSource
	modDir := os.Getenv("FUTI_MODULES_DIR")
	if modDir == "" {
		modDir, err = config.DefaultModulesDir()
		if err != nil {
			log.Fatalf("modules dir: %v", err)
		}
	}
	return discovery.NewGlobalSource(modDir)
}

// runList выводит содержимое namespace или команды модуля.
// Если namespace резолвится как модуль — показывает команды (-r игнорируется).
// Иначе — показывает непосредственных детей namespace, или всё поддерево при recurse.
// При verbose=true добавляет колонку PATH.
func runList(namespace string, verbose, recurse bool) error {
	source := createSource()

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

	var rows [][]string
	for name, cmd := range mod.Commands {
		if verbose {
			rows = append(rows, []string{name, cmd.Description, manifestPath})
		} else {
			rows = append(rows, []string{name, cmd.Description})
		}
	}

	w := tablewriter.NewTable(os.Stdout)
	if verbose {
		w.Header("COMMAND", "DESCRIPTION", "PATH")
	} else {
		w.Header("COMMAND", "DESCRIPTION")
	}
	if err := w.Bulk(rows); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return w.Render()
}

func printChildren(children []discovery.Child, verbose bool) error {
	var rows [][]string
	for _, c := range children {
		var desc, path string
		if c.Type == "module" {
			path = c.ManifestPath
			if mod, err := execmodule.LoadModule(c.ManifestPath, c.ModuleRoot); err == nil {
				desc = mod.Description
			}
		}
		if verbose {
			rows = append(rows, []string{c.Type, c.Name, desc, path})
		} else {
			rows = append(rows, []string{c.Type, c.Name, desc})
		}
	}

	w := tablewriter.NewTable(os.Stdout)
	if verbose {
		w.Header("TYPE", "NAME", "DESCRIPTION", "PATH")
	} else {
		w.Header("TYPE", "NAME", "DESCRIPTION")
	}
	if err := w.Bulk(rows); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return w.Render()
}

func printUsage() {
	fmt.Fprint(os.Stderr, `futi — universal CLI utility manager

Turns scattered commands, scripts and tools into YAML modules with a single
CLI interface. Install a module once — use it everywhere.

USAGE
  futi <namespace...> <command> [--param val] [--param=val] [--bool-flag]
  futi <namespace...> list [flags]
  futi list [flags]
  futi -h | --help

The last positional argument (without --) is the command; everything before
it is the namespace, joined with dots. Example: "futi dev deploy staging"
resolves to namespace "dev.deploy", command "staging".

MODULES
  Modules live in ~/.config/futi/modules/ by default (override with
  FUTI_MODULES_DIR). With a config.yaml, sources can be anywhere —
  see CONFIGURATION below.
  A module is either a single YAML file (docker.yaml) or a package directory
  with a config.yaml inside (deploy/config.yaml). Nested namespaces map to
  directory paths: dev/deploy.yaml → namespace "dev.deploy".

CONFIGURATION
  A config file at ~/.config/futi/config.yaml can define multiple module
  sources with mountpoints:

    sources:
      - path: ~/.config/futi/modules         # root mount (default)
      - path: /path/to/project-modules       # mountpoint: ns1.ns2
        mountpoint: ns1.ns2
      - path: /path/to/tools                 # mounted as namespace "tools"
        mountpoint: dev
        mount_as_namespace: tools

  mountpoint       Mount source contents under this namespace (default: root)
  mount_as_namespace  Wrap source contents under an extra namespace level

  When no config file exists, futi falls back to a single source at
  ~/.config/futi/modules/ (or FUTI_MODULES_DIR).

LISTING
  futi list                        Show top-level modules and namespaces
  futi <ns> list                   Show children of a namespace or commands
                                   of a module
  futi list -r, --recurse          Recursively traverse the subtree
  futi list -v, --verbose          Add a PATH column (manifest location)
  futi <ns> list -r -v             Flags combine

PARAMETERS
  --param value                    Pass a string parameter
  --param=value                    Alternative syntax
  --bool-flag                      Boolean flag (true when bare)
  --bool-flag=false                Explicit false

  Boolean values are normalized: "false", "0", "no", "" are falsy;
  "true", "1", "yes" are truthy. Use {{if .flag}}...{{end}} in templates.

TEMPLATING
  Commands use Go text/template syntax:
    {{.name}}                       Substitute a parameter value
    {{if .follow}}...{{end}}        Conditional block (for bool params)

  Quote values with spaces in the template:
    cmd: "echo \"{{.msg}}\""        --msg "hello world" works correctly

MODULE SCHEMA (YAML)
  name: docker                     Optional, defaults to file/dir name
  description: Docker management    Optional
  commands:
    build:
      description: Build image      Optional
      params:
        - name: tag
          type: string              string (default) or bool
          required: true
          default: Dockerfile
      cmd: "docker build -t {{.tag}} ."

FLAGS
  -h, --help                        Show this help
  -v, --verbose                     Show module paths in list output
  -r, --recurse                     Recursively list namespace subtree

EXAMPLES
  futi docker build --tag myapp    Run "build" command in "docker" module
  futi dev deploy staging           Namespace "dev.deploy", command "staging"
  futi k8s logs mypod --follow      Bool flag --follow
  futi images list -r               Recursively list all image modules
  futi docker list -v               Show docker commands with manifest path

ENVIRONMENT
  FUTI_MODULES_DIR                  Override modules directory (used when
                                    no config.yaml exists; default
                                    ~/.config/futi/modules/)
`)
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

	source := createSource()
	if err := modulerunner.Run(context.Background(), source, namespace, command, params, nil, nil); err != nil {
		log.Fatalf("run: %v", err)
	}
}
