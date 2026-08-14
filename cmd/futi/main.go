package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// parsedArgs — результат парсинга аргументов CLI.
type parsedArgs struct {
	namespace string
	command   string
	params    map[string]string
	args      string   // строка после -- (как есть, joined)
	argsList  []string // args после shlex-разбора
}

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
// `--` — разделитель: всё после него = args команды (через shlex).
// "list" как последний аргумент — запрос листинга.
func parseArgs(args []string) (*parsedArgs, error) {
	params := make(map[string]string)
	var positional []string
	var argsList []string

	i := 0
	doubleDashSeen := false
	for i < len(args) {
		arg := args[i]

		if doubleDashSeen {
			argsList = append(argsList, arg)
			i++
			continue
		}

		if arg == "--" {
			doubleDashSeen = true
			i++
			continue
		}

		if arg == "-h" || arg == "--help" {
			return nil, errHelp
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
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") && args[i+1] != "--" {
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

	// args строка: join через пробел (сохраняет разделение для шаблона)
	argsStr := strings.Join(argsList, " ")

	if len(positional) == 0 {
		// Только args, без namespace/command — нужен default cmd модуля.
		// Но без namespace мы не знаем, какой модуль. Это ошибка, если
		// только не вызывается как `futi -- args` (без модуля).
		if argsStr == "" {
			return nil, errors.New("usage: futi <namespace...> <command> [--param val] [-- args]")
		}
		return nil, errors.New("usage: futi <namespace...> <command> [--param val] [-- args]")
	}

	if positional[len(positional)-1] == "list" {
		ns := strings.Join(positional[:len(positional)-1], ".")
		return nil, &listRequest{
			namespace: ns,
			verbose:   params["verbose"] == "true",
			recurse:   params["recurse"] == "true",
		}
	}

	if len(positional) < 2 {
		// Один позиционный — это namespace без команды (default cmd).
		// Допустимо, если у модуля есть cmd.
		return &parsedArgs{
			namespace: positional[0],
			command:   "",
			params:    params,
			args:      argsStr,
			argsList:  argsList,
		}, nil
	}

	command := positional[len(positional)-1]
	namespace := strings.Join(positional[:len(positional)-1], ".")

	return &parsedArgs{
		namespace: namespace,
		command:   command,
		params:    params,
		args:      argsStr,
		argsList:  argsList,
	}, nil
}

// createSource создаёт источник модулей.
// Приоритет: локальный source (./.futi/ или ./futi.yaml) поверх глобального.
// Если есть config.yaml — загружает конфиг и создаёт CompositeSource.
// Иначе — fallback на GlobalSource с ~/.config/futi/modules/ (или FUTI_MODULES_DIR).
func createSource() discovery.Source {
	global := createGlobalSource()
	local := localSource()
	if local != nil {
		return discovery.NewLayeredSource(local, global)
	}
	return global
}

// createGlobalSource создаёт глобальный источник модулей из config.yaml
// или fallback на ~/.config/futi/modules/.
func createGlobalSource() discovery.Source {
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

// localSource проверяет CWD на наличие локальных модулей:
//   - ./.futi/ — каталог модулей (GlobalSource)
//   - ./futi.yaml — одиночный файл модуля (FileSource, имя из поля name)
//
// Возвращает nil, если локальных модулей нет.
// При наличии обоих — ./.futi/ имеет приоритет над ./futi.yaml.
func localSource() discovery.Source {
	var layers []discovery.Source

	if info, err := os.Stat(".futi"); err == nil && info.IsDir() {
		abs, _ := filepath.Abs(".futi")
		layers = append(layers, discovery.NewGlobalSource(abs))
	}

	if _, err := os.Stat("futi.yaml"); err == nil {
		mod, err := execmodule.LoadModule("futi.yaml", ".")
		if err == nil {
			name := mod.Name
			if name == "" {
				name = "futi"
			}
			abs, _ := filepath.Abs("futi.yaml")
			absRoot, _ := filepath.Abs(".")
			layers = append(layers, discovery.NewFileSource(abs, absRoot, name))
		}
	}

	if len(layers) == 0 {
		return nil
	}
	if len(layers) == 1 {
		return layers[0]
	}
	return discovery.NewLayeredSource(layers...)
}

// runList выводит содержимое namespace или команды модуля.
// Если namespace резолвится как модуль — показывает команды (-r игнорируется).
// Если namespace содержит путь к команде с subcommands — показывает subcommands.
// Иначе — показывает непосредственных детей namespace, или всё поддерево при recurse.
// При verbose=true добавляет колонку PATH.
func runList(w io.Writer, source discovery.Source, namespace string, verbose, recurse bool) error {
	// Сначала пробуем резолвить как модуль
	manifestPath, moduleRoot, err := source.Resolve(namespace)
	if err == nil {
		return printCommands(w, manifestPath, moduleRoot, verbose)
	}

	// Не модуль — пробуем progressive resolution для subcommands
	if namespace != "" {
		segments := strings.Split(namespace, ".")
		for i := len(segments) - 1; i > 0; i-- {
			ns := strings.Join(segments[:i], ".")
			commandPath := segments[i:]

			manifestPath, moduleRoot, err := source.Resolve(ns)
			if err != nil {
				continue
			}

			mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}

			cmd, err := mod.ResolveCommandPath(commandPath)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}

			if len(cmd.Commands) > 0 {
				return renderCommandTable(w, cmd.Commands, manifestPath, verbose)
			}

			return fmt.Errorf("list: command %q has no subcommands", strings.Join(commandPath, " "))
		}
	}

	// Не модуль и не subcommand — пробуем как namespace
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
		fmt.Fprintln(w, "No modules found.")
		return nil
	}
	return printChildren(w, children, verbose)
}

func printCommands(w io.Writer, manifestPath, moduleRoot string, verbose bool) error {
	mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	return renderCommandTable(w, mod.Commands, manifestPath, verbose)
}

// renderCommandTable выводит map команд в виде отсортированной таблицы.
// Используется для top-level команд модуля и для subcommands.
func renderCommandTable(w io.Writer, commands map[string]*execmodule.Command, manifestPath string, verbose bool) error {
	if len(commands) == 0 {
		fmt.Fprintln(w, "No commands found.")
		return nil
	}

	var rows [][]string
	for name, cmd := range commands {
		if verbose {
			rows = append(rows, []string{name, cmd.Description, manifestPath})
		} else {
			rows = append(rows, []string{name, cmd.Description})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	tw := tablewriter.NewTable(w)
	if verbose {
		tw.Header("COMMAND", "DESCRIPTION", "PATH")
	} else {
		tw.Header("COMMAND", "DESCRIPTION")
	}
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return tw.Render()
}

func printChildren(w io.Writer, children []discovery.Child, verbose bool) error {
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

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][1] < rows[j][1]
	})

	tw := tablewriter.NewTable(w)
	if verbose {
		tw.Header("TYPE", "NAME", "DESCRIPTION", "PATH")
	} else {
		tw.Header("TYPE", "NAME", "DESCRIPTION")
	}
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return tw.Render()
}

func printUsage() {
	fmt.Fprint(os.Stderr, `futi — universal CLI utility manager

Turns scattered commands, scripts and tools into YAML modules with a single
CLI interface. Install a module once — use it everywhere.

USAGE
  futi <namespace...> <command> [--param val] [--param=val] [--bool-flag] [-- args]
  futi <namespace>                    Invoke module's default command (if defined)
  futi <namespace...> list [flags]
  futi list [flags]
  futi -h | --help

The last positional argument (without --) is the command; everything before
it is the namespace, joined with dots. Example: "futi dev deploy staging"
resolves to namespace "dev.deploy", command "staging".

Arguments after "--" are passed to the command's template as {{.args}}
(space-joined string) and {{.args_list}} (array). Example:
  futi echo_mod echo -- hello world  →  cmd: "echo {{.args}}"  → echo hello world

Note: the "--" separator for args may be replaced by a hybrid mode in
future versions (positional args for leaf commands without explicit "--").

MODULES
  Modules live in ~/.config/futi/modules/ by default (override with
  FUTI_MODULES_DIR). With a config.yaml, sources can be anywhere —
  see CONFIGURATION below.
  A module is either a single YAML file (docker.yaml) or a package directory
  with a config.yaml inside (deploy/config.yaml). Nested namespaces map to
  directory paths: dev/deploy.yaml → namespace "dev.deploy".

  Auto-discovery: futi also searches the current working directory for
  ./futi.yaml (single module) or ./.futi/ (module directory). Local modules
  override global ones with the same name.

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
  futi <ns> <cmd> list             Show subcommands of a command
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
  cmd: "docker ps"                  Optional default command (invoked when
                                   no command is specified)
  commands:
    build:
      description: Build image      Optional
      params:
        - name: tag
          type: string              string (default), bool, or int
          required: true
          default: Dockerfile
      cmd: "docker build -t {{.tag}} ."

    # Nested commands: a command can have "commands" alongside or instead
    # of "cmd". When both are present, "cmd" is a fallback (invoked when
    # no subcommand matches).
    migrate:
      description: Database migrations
      cmd: "migrate"                 # fallback if no subcommand given
      commands:
        up:
          cmd: "migrate up"
        down:
          cmd: "migrate down"
        create:
          params:
            - name: name
              type: string
              required: true
          cmd: "migrate create {{.name}}"

  # Module with default command + args (echo-like):
  #   name: echo_mod
  #   cmd: "echo {{.args}}"
  #   commands:
  #     upper:
  #       cmd: "echo {{.args}} | tr a-z A-Z"

TEMPLATING
  Commands use Go text/template syntax:
    {{.name}}                       Substitute a parameter value
    {{.args}}                       All args after "--" (space-joined string)
    {{index .args_list 0}}          Nth arg after "--" (array access)
    {{if .follow}}...{{end}}        Conditional block (for bool params)

  Quote values with spaces in the template:
    cmd: "echo \"{{.msg}}\""        --msg "hello world" works correctly

FLAGS
  -h, --help                        Show this help
  -v, --verbose                     Show module paths in list output
  -r, --recurse                     Recursively list namespace subtree

EXAMPLES
  futi docker build --tag myapp    Run "build" command in "docker" module
  futi dev deploy staging           Namespace "dev.deploy", command "staging"
  futi k8s logs mypod --follow      Bool flag --follow
  futi db migrate up                Nested command: "migrate up" in "db" module
  futi db migrate list              List subcommands of "migrate"
  futi echo_mod echo -- hello world Pass args to command (via {{.args}})
  futi echo_mod -- hello world      Invoke module's default command with args
  futi images list -r               Recursively list all image modules
  futi docker list -v               Show docker commands with manifest path

  Exit codes: futi propagates the exit code of the called command.
  If docker build exits with 5, futi exits with 5.

ENVIRONMENT
  FUTI_MODULES_DIR                  Override modules directory (used when
                                    no config.yaml exists; default
                                    ~/.config/futi/modules/)
`)
}

// exitCodeFromError извлекает код завершения из ошибки.
// Если ошибка содержит *exec.ExitError — возвращает её ExitCode().
// Иначе — 1 (стандартный код ошибки для futi).
func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	pa, err := parseArgs(os.Args[1:])
	if err != nil {
		var req *listRequest
		switch {
		case errors.As(err, &req):
			source := createSource()
			if listErr := runList(os.Stdout, source, req.namespace, req.verbose, req.recurse); listErr != nil {
				log.Fatalf("list: %v", listErr)
			}
			return
		case errors.Is(err, errListCommand):
			source := createSource()
			if listErr := runList(os.Stdout, source, "", false, false); listErr != nil {
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
	if err := modulerunner.Run(context.Background(), source, pa.namespace, pa.command, pa.params, pa.args, pa.argsList, nil, nil); err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(exitCodeFromError(err))
	}
}
