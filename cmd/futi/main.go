package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tivjnb/futi/internal/completion"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/modulecache"
	"github.com/tivjnb/futi/internal/modulerunner"
	"github.com/tivjnb/futi/internal/termtext"

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
	flags     []string // сырые флаги между путём и `--` (привязка — bindFlags)
	args      string   // строка после -- (как есть, joined)
	argsList  []string // args после shlex-разбора
	dryRun    bool     // глобальный --dry-run: напечатать команду, не исполняя
}

// listRequest сигнализирует, что запрошен листинг namespace или модуля.
type listRequest struct {
	namespace string
	verbose   bool
	recurse   bool
}

func (e *listRequest) Error() string { return "list command requested" }
func (e *listRequest) Unwrap() error { return errListCommand }

// helpRequest сигнализирует, что запрошена справка (последний аргумент "help").
// positional — все сегменты до "help" (namespace + command path). Диспетчер
// решает: если "<positional> help" резолвится как user-defined команда —
// выполнить её (с flags/args/argsList, как обычную), иначе системная справка
// (флаги и args при ней игнорируются). flags — сырые токены после пути, их
// привязка к параметрам по схеме команды — в bindFlags.
type helpRequest struct {
	positional []string
	flags      []string
	args       string
	argsList   []string
	dryRun     bool // глобальный --dry-run (переносится в пользовательскую help)
}

func (e *helpRequest) Error() string { return "help requested" }

// parseArgs разбирает аргументы CLI в строгом порядке: путь → флаги → `-- args`.
// Путь — подряд идущие слова от начала до первого флагового токена (начинается
// с "-" и длиннее 1 символа; одиночный "-" — обычное слово). Последнее слово
// пути = команда, остальные = namespace через ".". Токены после пути до `--` —
// сырые флаги: привязка к параметрам по схеме команды — в bindFlags.
// Всё после первого `--` = args команды.
// "list" / "help" как последнее слово пути — системные запросы;
// у list допустимы только свои флаги.
func parseArgs(args []string) (*parsedArgs, error) {
	var path, flags, argsList []string

	i := 0
	for ; i < len(args) && !isFlagToken(args[i]) && args[i] != "--"; i++ {
		path = append(path, args[i])
	}
	for ; i < len(args); i++ {
		if args[i] == "--" {
			argsList = append(argsList, args[i+1:]...)
			break
		}
		flags = append(flags, args[i])
	}

	if len(path) == 0 {
		if hasHelpFlag(flags) {
			return nil, errHelp
		}
		usage := "usage: futi <namespace...> <command> [--param value] [-- args]"
		if len(flags) > 0 {
			usage += " (" + pathBeforeFlagsHint + ")"
		}
		return nil, errors.New(usage)
	}

	switch path[len(path)-1] {
	case "list":
		return nil, parseListRequest(path[:len(path)-1], flags)
	case "help":
		// В отличие от list, help НЕ зарезервирован: диспетчер проверяет,
		// нет ли user-defined команды "help" (см. runHelp). Сырые флаги и args
		// сохраняются для неё; системная справка их игнорирует.
		return nil, &helpRequest{
			positional: splitDots(path[:len(path)-1]),
			flags:      flags,
			args:       strings.Join(argsList, " "),
			argsList:   argsList,
		}
	}

	// args строка: join через пробел (сохраняет разделение для шаблона)
	pa := &parsedArgs{
		flags:    flags,
		args:     strings.Join(argsList, " "),
		argsList: argsList,
	}
	pa.namespace, pa.command = splitPath(path)
	return pa, nil
}

// splitDots делит каждый сегмент пути по ".", чтобы `docker.build help`
// вёл себя как `docker build help`.
func splitDots(path []string) []string {
	var out []string
	for _, seg := range path {
		out = append(out, strings.Split(seg, ".")...)
	}
	return out
}

// splitPath делит позиционный путь на namespace и команду. Один сегмент —
// namespace без команды (default cmd модуля); иначе последний сегмент — команда.
func splitPath(path []string) (namespace, command string) {
	if len(path) == 1 {
		return path[0], ""
	}
	return strings.Join(path[:len(path)-1], "."), path[len(path)-1]
}

// isFlagToken: токен — флаг, если начинается с "-" и длиннее одного символа.
func isFlagToken(tok string) bool {
	return len(tok) > 1 && tok[0] == '-'
}

// hasBareWord: среди сырых флагов есть слово без "-" (признак слова пути
// после флага, например `futi k8s --follow logs`).
// pathBeforeFlagsHint — подсказка про порядок: путь команды до флагов.
const pathBeforeFlagsHint = "the command path goes before flags"

// withPathHint дописывает подсказку о порядке пути и флагов, если среди флагов
// есть «голое» слово (путь после флага); повторно не добавляет.
func withPathHint(msg string, flags []string) string {
	if hasBareWord(flags) && !strings.Contains(msg, pathBeforeFlagsHint) {
		return msg + " (" + pathBeforeFlagsHint + ")"
	}
	return msg
}

func hasBareWord(flags []string) bool {
	for _, f := range flags {
		if !isFlagToken(f) {
			return true
		}
	}
	return false
}

func hasHelpFlag(flags []string) bool {
	for _, f := range flags {
		if f == "-h" || f == "--help" {
			return true
		}
	}
	return false
}

// parseListRequest разбирает флаги запроса list: -v/--verbose, -r/--recurse,
// -h/--help (справка). Любой другой флаг — ошибка.
func parseListRequest(namespace, flags []string) error {
	if hasHelpFlag(flags) {
		return errHelp
	}
	req := &listRequest{namespace: strings.Join(namespace, ".")}
	for _, f := range flags {
		switch f {
		case "-v", "--verbose":
			req.verbose = true
		case "-r", "--recurse":
			req.recurse = true
		default:
			return fmt.Errorf("list: unknown flag %q (supported: -v, --verbose, -r, --recurse)", f)
		}
	}
	return req
}

// cacheSubcommand извлекает cache subcommand из args (os.Args[1:]).
// Возвращает (subcommand, true) если первый аргумент — "cache".
// Возвращает ("", false) если "cache" не первый аргумент (обычная команда).
// subcommand="" означает "cache" без подкоманды — main выведет понятную ошибку.
func cacheSubcommand(args []string) (subcommand string, present bool) {
	if len(args) < 1 || args[0] != "cache" {
		return "", false
	}
	if len(args) < 2 {
		return "", true
	}
	return args[1], true
}

// runCacheRefresh явно обновляет кэш: обходит globalSource, перечитывает
// изменившиеся модули, сохраняет на диск. Выводит статистику.
// При verbose=true дополнительно выводит списки обновлённых и пропущенных
// модулей (фикс #15).
func runCacheRefresh(w io.Writer, cache *modulecache.Cache, globalSource discovery.Source, verbose bool) error {
	result, err := core.RefreshCache(cache, globalSource)
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return err
	}
	// Частичный листинг: сводка по читаемым sources выводится, ошибка
	// возвращается в конце — вызывающий покажет предупреждения.
	fmt.Fprintf(w, "Cache refreshed: %d modules, %d updated", result.Total, result.Updated)
	if result.Skipped > 0 {
		fmt.Fprintf(w, ", %d skipped", result.Skipped)
	}
	fmt.Fprintln(w)

	if verbose {
		if len(result.UpdatedModules) > 0 {
			fmt.Fprintln(w, "Updated:")
			for _, path := range result.UpdatedModules {
				fmt.Fprintf(w, "  %s\n", path)
			}
		}
		if len(result.SkippedModules) > 0 {
			fmt.Fprintln(w, "Skipped:")
			for _, sm := range result.SkippedModules {
				fmt.Fprintf(w, "  %s (%s)\n", sm.Path, sm.Reason)
			}
		}
	}
	return err
}

// runCacheStatus выводит текущее состояние кэша: политику, время последнего
// обновления, количество модулей, путь к gob-файлу (фикс #17).
func runCacheStatus(w io.Writer, cache *modulecache.Cache, cachePath string) error {
	st := core.CacheStatusOf(cache, cachePath)
	fmt.Fprintf(w, "Policy: %s\n", st.Policy)

	if st.LastRefresh.IsZero() {
		fmt.Fprintln(w, "Last refresh: never")
	} else {
		fmt.Fprintf(w, "Last refresh: %s\n", st.LastRefresh.Format("2006-01-02 15:04:05"))
	}

	fmt.Fprintf(w, "Modules: %d\n", st.Modules)

	if st.Path == "" {
		fmt.Fprintln(w, "Storage: in-memory (no persistent cache)")
	} else {
		fmt.Fprintf(w, "Cache file: %s\n", st.Path)
	}
	return nil
}

// runCacheClear удаляет gob-файл кэша с диска (фикс #17).
// In-memory кэш не очищается — он живёт только в текущем процессе.
func runCacheClear(w io.Writer, cachePath string) error {
	if cachePath == "" {
		fmt.Fprintln(w, "cache: in-memory only, nothing to clear")
		return nil
	}
	if err := core.ClearCache(cachePath); err != nil {
		return err
	}
	fmt.Fprintf(w, "Cache cleared: %s\n", cachePath)
	return nil
}

// runList выводит содержимое namespace или команды модуля.
// Если namespace резолвится как модуль — показывает команды (-r игнорируется).
// Если namespace содержит путь к команде с subcommands — показывает subcommands.
// Иначе — показывает непосредственных детей namespace, или всё поддерево при recurse.
// При verbose=true добавляет колонку PATH.
// loader используется для загрузки модулей (с кэшем или без).
func runList(w io.Writer, source discovery.Source, namespace string, verbose, recurse bool, loader modulerunner.ModuleLoader) error {
	res, err := core.ListIn(source, loader, namespace, recurse)
	// Частичный листинг: результат валиден, выводим его и возвращаем ошибку,
	// чтобы вызывающий показал предупреждения.
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return err
	}
	if res.Kind == core.ListNamespace {
		if len(res.Children) == 0 {
			fmt.Fprintln(w, "No modules found.")
			return err
		}
		if rerr := renderChildren(w, res.Children, verbose); rerr != nil {
			return rerr
		}
		return err
	}
	return renderCommandTable(w, res, verbose)
}

func printCommands(w io.Writer, manifestPath, moduleRoot string, verbose bool, loader modulerunner.ModuleLoader) error {
	res, err := core.ModuleCommands(loader, manifestPath, moduleRoot)
	if err != nil {
		return err
	}
	return renderCommandTable(w, res, verbose)
}

// runValidate загружает модуль и выводит отчёт валидации схемы.
// Использует ParseModule (без валидации) + Validate() для получения полного
// списка ошибок и предупреждений за один проход.
// Возвращает error только если есть errors (warnings не блокируют).
func runValidate(w io.Writer, source discovery.Source, namespace string) error {
	rep, err := core.ValidateIn(source, namespace)
	if err != nil {
		return err
	}

	report, hasErrors := execmodule.FormatValidateReport(rep.Errors, rep.Warnings)
	fmt.Fprint(w, report)

	if hasErrors {
		return fmt.Errorf("validate: module %q has %d error(s)", namespace, len(rep.Errors))
	}
	return nil
}

// renderCommandTable prints commands as a table, used for a module's
// top-level commands and for subcommands.
// res.HasDefault adds a (default) row first in the table (the module's
// default cmd or the subcommand's fallback cmd).
func renderCommandTable(w io.Writer, res core.ListResult, verbose bool) error {
	if len(res.Commands) == 0 && !res.HasDefault {
		fmt.Fprintln(w, "No commands found.")
		return nil
	}

	var rows [][]string
	for _, c := range res.Commands {
		if verbose {
			rows = append(rows, []string{termtext.Line(c.Name), termtext.Line(c.Description), termtext.Line(res.Manifest)})
		} else {
			rows = append(rows, []string{termtext.Line(c.Name), termtext.Line(c.Description)})
		}
	}

	// Sort by the sanitized name.
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	if res.HasDefault {
		var defaultRow []string
		if verbose {
			defaultRow = []string{"(default)", "", termtext.Line(res.Manifest)}
		} else {
			defaultRow = []string{"(default)", ""}
		}
		rows = append([][]string{defaultRow}, rows...)
	}

	tw := tablewriter.NewTable(w)
	if verbose {
		tw.Header("COMMAND", "DESCRIPTION", "PATH")
	} else {
		tw.Header("COMMAND", "DESCRIPTION")
	}
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	if err := tw.Render(); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return nil
}

func printChildren(w io.Writer, children []discovery.Child, verbose bool, loader modulerunner.ModuleLoader) error {
	return renderChildren(w, core.DescribeChildren(children, loader), verbose)
}

// renderChildren prints the children of a namespace as a table.
func renderChildren(w io.Writer, children []core.ChildEntry, verbose bool) error {
	var rows [][]string
	for _, c := range children {
		typ := "namespace"
		var desc, path string
		if c.IsModule {
			typ = "module"
			path = termtext.Line(c.Path)
			if c.Conflict && len(c.ConflictSources) > 0 {
				// Модуль с одним именем в нескольких sources.
				desc = "conflict: module in several sources: " + termtext.Line(strings.Join(c.ConflictSources, ", "))
			} else if c.Conflict {
				// ManifestPath конфликтной записи — от x.yaml; c.Name в `list -r` —
				// полный путь (ns.x), поэтому имена файлов берём из пути.
				base := filepath.Base(c.Path)
				desc = termtext.Line("conflict: " + base + " and " + strings.TrimSuffix(base, ".yaml") + "/config.yaml")
			} else if c.LoadErr != nil {
				// Битый модуль не должен быть неотличим от модуля без описания.
				// Полную причину (с путём) не печатаем: колонка раздувается, путь
				// есть в PATH при -v, причину покажет validate.
				desc = "(error: cannot load module; run 'futi modules validate <namespace>')"
			} else {
				desc = termtext.Line(c.Description)
			}
		}
		if verbose {
			rows = append(rows, []string{typ, termtext.Line(c.Name), desc, path})
		} else {
			rows = append(rows, []string{typ, termtext.Line(c.Name), desc})
		}
	}

	// По имени, при равных именах (модуль и namespace x) — по типу.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i][1] != rows[j][1] {
			return rows[i][1] < rows[j][1]
		}
		return rows[i][0] < rows[j][0]
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
	if err := tw.Render(); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return nil
}

// printUsage печатает общую справку в w: stdout по явному запросу (-h, help),
// stderr — когда futi запущен без аргументов.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `futi — universal CLI utility manager

Turns scattered commands, scripts and tools into YAML modules with a single
CLI interface. Install a module once — use it everywhere.

USAGE
  futi <namespace...> <command> [--param value] [--bool-flag] [-- args]
  futi <namespace>                    Invoke module's default command (if defined)
  futi <namespace...> list [flags]
  futi list [flags]
  futi modules validate <namespace>   Validate module schema
  futi modules init <name> [flags]    Create a new module from template
  futi modules <subcommand>           Manage modules (install, uninstall, ...)
  futi cache refresh                  Refresh module cache
  futi completion <bash|zsh|fish>     Generate a shell completion script
  futi --dry-run <path...> <command>  Print the command without running it
  futi -h | --help
  futi --version                      Print version

The last positional argument (without --) is the command; everything before
it is the namespace, joined with dots. Example: "futi dev deploy staging"
resolves to namespace "dev.deploy", command "staging".

The order is strict: path, then flags, then "-- args". The path ends at the
first token starting with "-"; a word after a flag is only accepted as the
value of the preceding string/int flag. "-h" / "--help" after the path shows
help for that path.

Reserved words: "list", "cache", "modules", "completion", "__complete". They
cannot be used as a module name or as any segment of a module path, and "list"
cannot be a command name in a module.

Arguments after "--" are passed to the command's template as {{.args}}
(space-joined string) and {{.args_list}} (array). Example:
  futi echo_mod echo -- hello world  →  cmd: "echo {{.args}}"  → echo hello world

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

  path              Module directory; ~/... is relative to the home dir (the bare
                    home dir needs quotes: "~", a bare ~ is YAML null), a
                    relative path is resolved from the directory of config.yaml
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

  The (default) marker in command listings indicates the module's default
  command (cmd field), invoked when no command is specified.

CACHE
  futi cache refresh               Reload modules from source into cache
  futi cache refresh -v, --verbose Show which modules were updated/skipped
  futi cache status                Show cache policy, last refresh, module count
  futi cache clear                 Remove the persistent cache file from disk

COMPLETION
  futi completion bash|zsh|fish    Print a shell completion script to stdout.
                                   Pipe it into your shell's completion setup:
                                     bash:  futi completion bash > /etc/bash_completion.d/futi
                                     zsh:   futi completion zsh > "${fpath[1]}/_futi"
                                     fish:  futi completion fish > ~/.config/fish/completions/futi.fish
                                   Completion suggests modules, namespaces,
                                   commands, subcommands, parameter names and
                                   boolean values (true/false).

VALIDATION
  futi modules validate <ns>       Validate module schema (all commands and
                                   params, recursively). Prints a report with
                                   ERROR and WARNING lines, or "OK" if clean.
                                   Exit code: 0 if no errors, 1 otherwise.

                                   Errors (block module loading):
                                     - command without cmd and commands
                                     - duplicate param names in a command
                                     - invalid param type (not string/bool/int)

                                   Warnings (do not block):
                                     - command with subcommands but no cmd
                                       (calling without subcommand fails)

MODULES
  futi modules init <name>         Create a new module in ./.futi/<name>.yaml
  futi modules init <ns>/<name>    Create with namespace (./.futi/<ns>/<name>.yaml)
  futi modules init <name> --global
                                   Create in ~/.config/futi/modules/<name>.yaml

  init flags:
  --desc <text>                    Module description (default: "My tool")
  --default-cmd <cmd>              Add cmd at module level (default command)
  --command <name>                 Add a command (repeatable)
  --global                         Create in global modules directory

  The generated module is valid and immediately discoverable by futi.
  If the file already exists, an error is returned (no overwrite).

  futi modules install <source>    Install a module from a local path, git URL
                                   or archive URL. Writes __futi_meta.yaml
                                   next to config.yaml and runs the module's
                                   attach: hook (if defined).
  futi modules uninstall <name>    Run detach: hook (if defined) and remove
                                   the installed module. Asks for confirmation.
  futi modules update [name]       Re-fetch the source and re-run attach:
                                   (all installed modules when no name given)
  futi modules info <name>         Show installed module details (from
                                   __futi_meta.yaml)
  futi modules list                List installed modules (with __futi_meta.yaml)
  futi modules path <name>         Print the module's directory (for scripts)
  futi modules add_command <module> <command>
                                   Add a command to the module's config.yaml
  futi modules remove_command <module> <command>
                                   Remove a command from config.yaml

  list, update and uninstall see modules in all config.yaml sources (names
  include the mountpoint); install and init --global write only to the global
  modules directory (or ./.futi/ with --local).

  install flags:
  --local                          Install into ./.futi/ instead of the
                                   global modules directory
  --namespace <ns>                 Install under a namespace (ns/name)
  --ref <branch|tag>               Git branch or tag to install from
  --from-local/--from-git/--from-archive
                                   Force source type detection
  --skip-attach                    Skip the attach: hook
  --yes, -y                        Run the attach: hook without asking
                                   (it asks for confirmation otherwise)

  update flags:
  --skip-attach                    Skip the attach: hook
  --yes, -y                        Replace changed module files and run the
                                   attach: hook without asking

  uninstall flags:
  --force                          Allow removing modules without __futi_meta.yaml
  --yes, -y                        Skip the confirmation prompt
  --skip-detach                    Skip the detach: hook
  --strict-detach                  Abort (do not delete) if detach: fails

PARAMETERS
  --param value                    Pass a string/int parameter
  --param=value                    Alternative syntax (required for values
                                   starting with "--")
  --bool-flag                      Boolean flag (true when bare; never takes
                                   the next word as its value)
  --bool-flag=false                Explicit false

  Boolean values: true/false, 1/0, yes/no, on/off (case-insensitive); anything
  else is an error. Use {{if .flag}}...{{end}} in templates.

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
  #     shout:
  #       cmd: "echo {{quote .args}}"

TEMPLATING
  Commands use Go text/template syntax:
    {{.name}}                       Substitute a parameter value
    {{.args}}                       All args after "--" (space-joined string)
    {{index .args_list 0}}          Nth arg after "--" (array access)
    {{.env.FOO}}                    Environment variable FOO (error if unset)
    {{if index .env "FOO"}}...{{end}}  Conditional on env var presence
    {{if .follow}}...{{end}}        Conditional block (for bool params)

    {{.module_root}}                Directory of the module (use {{quote .module_root}})
    {{.module_name}}                Name of the module

  Commands run without a shell: the rendered "cmd" is split into words (like
  shlex), so a substituted value is split again. Always wrap user input in
  quote, otherwise spaces split it into several arguments and a value starting
  with "-" becomes a flag:
    cmd: "echo {{quote .msg}}"      --msg "hello world" is passed as one argument
  Shell features (pipes, redirects, globs) are not available in "cmd".

FLAGS
  -h, --help                        Show this help (after a path: help for
                                    that path)
  -v, --verbose                     Show module paths (flag of "list" only)
  -r, --recurse                     Recursively list namespace subtree
                                    (flag of "list" only)

  Other short flags (-x) are not accepted: pass parameters as --name value.

EXAMPLES
  futi docker build -h             Show help for "docker build"
  futi docker build --tag myapp    Run "build" command in "docker" module
  futi dev deploy staging           Namespace "dev.deploy", command "staging"
  futi k8s logs --follow            Bool flag --follow
  futi db migrate up                Nested command: "migrate up" in "db" module
  futi db migrate list              List subcommands of "migrate"
  futi echo_mod echo -- hello world Pass args to command (via {{.args}})
  futi echo_mod -- hello world      Invoke module's default command with args
  futi images list -r               Recursively list all image modules
  futi docker list -v               Show docker commands with manifest path

  Exit codes: futi propagates the exit code of the called command.
  If docker build exits with 5, futi exits with 5.

ENVIRONMENT
  FUTI_MODULES_DIR                  Override modules directory (discovery
                                    uses it when no config.yaml exists;
                                    install without --local writes there;
                                    default ~/.config/futi/modules/)
`)
}

// interruptSignal возвращает прерывающий сигнал (SIGINT, SIGTERM или SIGHUP)
// из err: полученный futi и пересланный команде (*executor.InterruptedError —
// даже если команда его перехватила и вышла обычным кодом) или тот, которым
// убит дочерний процесс (*exec.ExitError). Для остальных сигналов (SIGQUIT,
// SIGSEGV и т.п.) и прочих ошибок — false.
func interruptSignal(err error) (syscall.Signal, bool) {
	var intErr *executor.InterruptedError
	if errors.As(err, &intErr) && isInterruptSignal(intErr.Signal) {
		return intErr.Signal, true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || !isInterruptSignal(ws.Signal()) {
		return 0, false
	}
	return ws.Signal(), true
}

// isInterruptSignal — сигнал, прерывающий futi и циклы шелла.
func isInterruptSignal(sig syscall.Signal) bool {
	switch sig {
	case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
		return true
	}
	return false
}

// exitAfterRunError завершает процесс после ошибки запуска команды. Если
// ребёнок убит прерывающим сигналом, futi поднимает тот же сигнал на себя и
// гибнет от него: шелл (bash) иначе решит, что команда сама обработала
// Ctrl+C, и не прервёт цикл. Код выхода шелла остаётся 128+N.
func exitAfterRunError(err error) {
	if sig, ok := interruptSignal(err); ok {
		signal.Reset(sig)
		// На Windows Signal вернёт ошибку: тогда сразу выходим с кодом.
		if p, findErr := os.FindProcess(os.Getpid()); findErr == nil {
			if p.Signal(sig) == nil {
				// Доставка сигнала асинхронна; обычно процесс умирает здесь.
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
	os.Exit(core.ExitCode(err))
}

// formatRunError форматирует ошибку выполнения команды для вывода в stderr.
// Для *execmodule.TemplateError (ошибка парсинга/выполнения cmd-шаблона) даёт
// дружелюбное многострочное сообщение без длинной цепочки обёрток и технического
// трейса text/template. Для остальных ошибок — "run: <err>" как раньше.
func formatRunError(err error) string {
	var tmplErr *execmodule.TemplateError
	if errors.As(err, &tmplErr) {
		return fmt.Sprintf(
			"error: failed to render template.\n  Template: %s\n  Cause: %v",
			tmplErr.Cmd, tmplErr.Cause,
		)
	}
	return fmt.Sprintf("run: %v", err)
}

// runErrorText — текст ошибки запуска: короткий, если причина уже напечатана
// предупреждением, иначе formatRunError. Ошибки шаблона не сокращаются.
func runErrorText(err error, path []string) string {
	var tmplErr *execmodule.TemplateError
	if !errors.As(err, &tmplErr) && coveredByWarning(err) {
		return coveredReadMessage("run", strings.Join(path, "."))
	}
	return formatRunError(err)
}

// withValidateHint дописывает к ошибке резолва подсказку о переезде validate
// (`futi <ns> validate` → `futi modules validate <ns>`), если путь оканчивается
// словом validate и команда/модуль не найдены. Конкретный <ns> подставляется,
// только если он резолвится в существующий модуль; иначе — общая форма.
// Прочие ошибки (в том числе исполнения пользовательской команды validate)
// возвращаются как есть.
func withValidateHint(msg string, err error, path []string, source discovery.Source) string {
	if !errors.Is(err, modulerunner.ErrModuleNotFound) && !errors.Is(err, execmodule.ErrCommandNotFound) {
		return msg
	}
	if len(path) == 0 || path[len(path)-1] != "validate" {
		return msg
	}
	ns := "<namespace>"
	if len(path) > 1 {
		cand := strings.Join(path[:len(path)-1], ".")
		if _, _, rerr := source.Resolve(cand); rerr == nil {
			ns = cand
		}
	}
	return msg + "\nhint: use 'futi modules validate " + ns + "'"
}

// commandPath восстанавливает позиционный путь (сегменты namespace + команда)
// разобранной команды — тот же, что Run получает как positional.
func commandPath(pa *parsedArgs) []string {
	path := strings.Split(pa.namespace, ".")
	if pa.command != "" {
		path = append(path, pa.command)
	}
	return path
}

// systemHelpCode печатает справку по пути в stdout и возвращает код выхода:
// 0 (в том числе при частичном листинге — предупреждения в stderr) или 1 с
// ошибкой в stderr.
func systemHelpCode(stdout, stderr io.Writer, source discovery.Source, loader modulerunner.ModuleLoader, path []string) int {
	if err := runSystemHelp(stdout, source, loader, path); err != nil {
		if printPartialWarnings(stderr, err) {
			return 0
		}
		msg := err.Error()
		if coveredByWarning(err) {
			msg = coveredReadMessage("help", strings.Join(path, "."))
		}
		fmt.Fprintf(stderr, "%s\n", withValidateHint(msg, err, path, source))
		return 1
	}
	return 0
}

// runParsed — общий путь исполнения команды (rt.Resolve → bindFlags
// (-h/--help → справка) → rt.Execute → formatRunError) для обычной ветки main и
// пользовательской help. Пишет в переданные stdout/stderr и возвращает код
// выхода. Второе значение — ошибка запуска (уже напечатанная в stderr): её
// возвращаем наружу, чтобы вызывающий через exitAfterRunError корректно
// обработал сигнальный путь (процесс гибнет тем же сигналом), а тесты
// проверяли код без os.Exit.
func runParsed(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, rt *core.Runtime, pa *parsedArgs) (int, error) {
	source, _ := rt.Source() // main has already checked the error
	loader := rt.Cache()
	// Схема команды нужна для привязки флагов (bool/string/int). -h/--help —
	// справка по пути, в том числе для веток без своей команды (futi docker -h).
	helpPath := commandPath(pa)
	t, err := rt.Resolve(pa.namespace, pa.command)
	if err != nil {
		if hasHelpFlag(pa.flags) {
			return systemHelpCode(stdout, stderr, source, loader, helpPath), nil
		}
		fmt.Fprintf(stderr, "%s\n", withPathHint(withValidateHint(runErrorText(err, helpPath), err, helpPath, source), pa.flags))
		return core.ExitCode(err), err
	}
	params, help, err := bindFlags(t.Command(), pa.flags)
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", withPathHint(err.Error(), pa.flags))
		return 1, nil
	}
	if help {
		return systemHelpCode(stdout, stderr, source, loader, helpPath), nil
	}
	res, err := rt.Execute(ctx, t, core.ExecuteRequest{
		Params: params, Args: pa.argsList,
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		DryRun: pa.dryRun,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", formatRunError(err))
		return core.ExitCode(err), err
	}
	if pa.dryRun {
		fmt.Fprintln(stdout, shellJoin(res.Argv))
		return 0, nil
	}
	if res.WaitErr != nil {
		fmt.Fprintf(stderr, "%s\n", formatRunError(res.WaitErr))
		return res.ExitCode, res.WaitErr
	}
	return 0, nil
}

// errDryRunScope — текст ошибки: --dry-run применим только к командам модулей.
const errDryRunScope = "futi: --dry-run applies only to module commands"

// stripDryRun снимает ведущий --dry-run из args (args[0] — имя программы).
// Флаг распознаётся только первым аргументом: дальше он обычный флаг модуля.
func stripDryRun(args []string) (rest []string, dryRun bool) {
	if len(args) > 1 && args[1] == "--dry-run" {
		return append([]string{args[0]}, args[2:]...), true
	}
	return args, false
}

// dryRunRejected сообщает, что при --dry-run первый аргумент относится к
// встроенным командам и быстрым путям, а не к командам модулей.
func dryRunRejected(args []string) bool {
	if len(args) < 2 {
		return true
	}
	switch args[1] {
	case "-h", "--help", "--version":
		return true
	}
	return completion.IsReservedName(args[1])
}

func main() {
	// --dry-run снимается первым, до всех быстрых путей.
	var dryRun bool
	os.Args, dryRun = stripDryRun(os.Args)
	if dryRun && dryRunRejected(os.Args) {
		fmt.Fprintln(os.Stderr, errDryRunScope)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	// Быстрый путь: -h/--help не требует config/source/cache.
	if os.Args[1] == "-h" || os.Args[1] == "--help" {
		printUsage(os.Stdout)
		return
	}

	// Быстрый путь: --version не требует config/source/cache.
	if os.Args[1] == "--version" {
		if len(os.Args) > 2 {
			fmt.Fprintln(os.Stderr, "futi: --version takes no arguments")
			os.Exit(1)
		}
		info, ok := debug.ReadBuildInfo()
		fmt.Println(versionString(version, info, ok))
		return
	}

	// Быстрый путь: futi modules init не требует config/source/cache.
	if os.Args[1] == "modules" && len(os.Args) > 2 && os.Args[2] == "init" {
		if err := runModulesInit(os.Stdout, os.Args[3:]); err != nil {
			fmt.Fprintf(os.Stderr, "modules init: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Быстрый путь: futi completion <shell> — только генерация скрипта,
	// без config/source/cache.
	if os.Args[1] == "completion" {
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "futi: completion requires a shell (valid: %s)\n", strings.Join(validCompletionShells, ", "))
			os.Exit(1)
		}
		if err := runCompletion(os.Stdout, os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "completion: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Runtime: config и глобальный source строятся сразу (ошибка конфига — до любых
	// команд), кэш и локальный source — лениво, при первом обращении.
	opts, err := core.DefaultOptions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "source: %v\n", err)
		os.Exit(1)
	}
	opts.Warn = func(msg string) { fmt.Fprintln(os.Stderr, msg) }
	rt, err := core.NewRuntime(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "source: %v\n", err)
		os.Exit(1)
	}

	// Быстрый путь: futi __complete <args...> — hidden-обработчик completion.
	// ДО cache.Start: __complete сам решает, нужно ли прогревать кэш
	// (самообновление при холодном/устаревшем), и не запускает Refresh по политике
	// на каждый TAB.
	if os.Args[1] == "__complete" {
		os.Exit(runCompleteMain(os.Stdout, os.Stderr, rt, os.Args[2:]))
	}

	// Быстрый путь: `futi modules <subcommand>` — до cache.Start, чтобы не
	// гонять Refresh по политике: мутирующие подкоманды сами инвалидируют
	// кэш, read-only обходятся загрузкой по требованию.
	if os.Args[1] == "modules" {
		cache := rt.Cache()
		source, err := rt.Source()
		if err != nil {
			fmt.Fprintf(os.Stderr, "source: %v\n", err)
			os.Exit(1)
		}
		if err := runModules(os.Stdout, os.Stdin, source, cache, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "modules: %v\n", err)
			// Прерванный хук — futi гибнет тем же сигналом; код хука, git
			// или curl наружу не пробрасывается: прочие ошибки дают 1.
			if _, ok := interruptSignal(err); ok {
				exitAfterRunError(err)
			}
			os.Exit(1)
		}
		return
	}

	// `futi cache <subcommand>` — fast path ДО cache.Start, чтобы избежать
	// двойного Refresh (Start с AlwaysPolicy + runCacheRefresh). Loading from disk
	// (Load) happens inside core.RefreshCache / core.CacheStatusOf; runCacheRefresh
	// is then the only Refresh, with a correct mtime+size comparison.
	if subcmd, ok := cacheSubcommand(os.Args[1:]); ok {
		cache := rt.Cache()
		cachePath := rt.CachePath()
		switch subcmd {
		case "refresh":
			// Парсим --verbose/-v из оставшихся аргументов.
			verbose := false
			for _, a := range os.Args[3:] {
				if a == "-v" || a == "--verbose" {
					verbose = true
				}
			}
			if err := runCacheRefresh(os.Stdout, cache, rt.GlobalSource(), verbose); err != nil && !printPartialWarnings(os.Stderr, err) {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			return
		case "status":
			if err := runCacheStatus(os.Stdout, cache, cachePath); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			return
		case "clear":
			if err := runCacheClear(os.Stdout, cachePath); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			return
		case "":
			fmt.Fprintln(os.Stderr, `futi: cache requires a subcommand (try "cache refresh", "cache status", or "cache clear")`)
			os.Exit(1)
		default:
			fmt.Fprintf(os.Stderr, "futi: unknown cache subcommand %q (try \"cache refresh\", \"cache status\", or \"cache clear\")\n", subcmd)
			os.Exit(1)
		}
	}

	// Создаём и запускаем кэш. Ошибки кэша не фатальны — CLI работает и без него.
	cache := rt.Cache()
	if err := rt.Start(); err != nil && !printPartialWarnings(os.Stderr, err) {
		fmt.Fprintf(os.Stderr, "cache: %v (continuing without cache)\n", err)
	}

	pa, err := parseArgs(os.Args[1:])
	if err != nil {
		var listReq *listRequest
		var helpReq *helpRequest
		switch {
		case dryRun && (errors.As(err, &listReq) || errors.Is(err, errHelp)):
			fmt.Fprintln(os.Stderr, errDryRunScope)
			os.Exit(1)
		case errors.As(err, &listReq):
			source, err := rt.Source()
			if err != nil {
				fmt.Fprintf(os.Stderr, "source: %v\n", err)
				os.Exit(1)
			}
			if listErr := runList(os.Stdout, source, listReq.namespace, listReq.verbose, listReq.recurse, cache); listErr != nil && !printPartialWarnings(os.Stderr, listErr) {
				fmt.Fprintln(os.Stderr, listErrorText(listErr, listReq.namespace))
				os.Exit(1)
			}
			return
		case errors.As(err, &helpReq):
			if _, err := rt.Source(); err != nil {
				fmt.Fprintf(os.Stderr, "source: %v\n", err)
				os.Exit(1)
			}
			helpReq.dryRun = dryRun
			code, runErr := runHelp(context.Background(), os.Stdin, os.Stdout, os.Stderr, rt, helpReq)
			if runErr != nil {
				exitAfterRunError(runErr)
			}
			if code != 0 {
				os.Exit(code)
			}
			return
		case errors.Is(err, errHelp):
			printUsage(os.Stdout)
			return
		default:
			fmt.Fprintf(os.Stderr, "parse: %v\n", err)
			os.Exit(1)
		}
	}

	if _, err := rt.Source(); err != nil {
		fmt.Fprintf(os.Stderr, "source: %v\n", err)
		os.Exit(1)
	}
	pa.dryRun = dryRun
	code, runErr := runParsed(context.Background(), os.Stdin, os.Stdout, os.Stderr, rt, pa)
	if runErr != nil {
		exitAfterRunError(runErr)
	}
	if code != 0 {
		os.Exit(code)
	}
}

// printedWarnings — ключи предупреждений (см. warningKey), уже выведенных в этом процессе:
// один и тот же нечитаемый source не должен предупреждать дважды (Start кэша и листинг).
var (
	printedWarningsMu sync.Mutex
	printedWarnings   = map[string]bool{}
	// warnedReads — первые *fs.PathError напечатанных предупреждений (под тем же
	// мьютексом): по ним итоговая ошибка с той же причиной сокращается.
	warnedReads []*fs.PathError
)

// printPartialWarnings печатает в w по строке `warning: <ошибка>` для каждой
// ошибки частичного листинга (*discovery.PartialError в цепочке err) и возвращает
// true. Уже выведенные в этом процессе тексты не повторяются. Для прочих ошибок
// (и nil) ничего не печатает и возвращает false.
func printPartialWarnings(w io.Writer, err error) bool {
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		return false
	}
	printedWarningsMu.Lock()
	defer printedWarningsMu.Unlock()
	for _, e := range pe.Errs {
		msg := e.Error()
		key := warningKey(e)
		if printedWarnings[key] {
			continue
		}
		printedWarnings[key] = true
		var pathErr *fs.PathError
		if errors.As(e, &pathErr) {
			warnedReads = append(warnedReads, pathErr)
		}
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
	return true
}

// warningKey — ключ дедупа предупреждений: первый *fs.PathError в цепочке
// (операция, путь, причина) — одна и та же проблема чтения приходит из разных
// листингов с разной обёрткой; без PathError — полный текст ошибки.
func warningKey(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Op + "\x00" + pathErr.Path + "\x00" + pathErr.Err.Error()
	}
	return err.Error()
}

// coveredByWarning сообщает, что ошибка уже объяснена напечатанным
// предупреждением: первый *fs.PathError в цепочке err имеет ту же причину, что и
// предупреждение, а пути совпадают или один лежит внутри другого. Операция
// (stat/open) не учитывается. Без *fs.PathError ошибка не покрыта.
func coveredByWarning(err error) bool {
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		return false
	}
	printedWarningsMu.Lock()
	defer printedWarningsMu.Unlock()
	for _, w := range warnedReads {
		if w.Err.Error() != pathErr.Err.Error() {
			continue
		}
		if pathWithin(w.Path, pathErr.Path) || pathWithin(pathErr.Path, w.Path) {
			return true
		}
	}
	return false
}

// pathWithin сообщает, что path совпадает с root или лежит внутри root.
func pathWithin(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	return path == root || strings.HasPrefix(path, strings.TrimRight(root, "/\\")+string(filepath.Separator))
}

// coveredReadMessage — короткое сообщение вместо ошибки, объяснённой предупреждением.
func coveredReadMessage(prefix, path string) string {
	return fmt.Sprintf("%s: cannot read namespace %q (see warning above)", prefix, path)
}

// listErrorText — текст итоговой ошибки list: короткий, если причина уже
// напечатана предупреждением, иначе полный.
func listErrorText(err error, namespace string) string {
	if coveredByWarning(err) {
		return coveredReadMessage("list", namespace)
	}
	return err.Error()
}
