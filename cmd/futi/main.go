package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"futi/internal/config"
	"futi/internal/discovery"
	"futi/internal/execmodule"
	"futi/internal/executor"
	"futi/internal/modulecache"
	"futi/internal/modulerunner"

	"github.com/olekukonko/tablewriter"
)

var (
	errListCommand     = errors.New("list command requested")
	errValidateCommand = errors.New("validate command requested")
	errHelp            = errors.New("help requested")
)

// parsedArgs — результат парсинга аргументов CLI.
type parsedArgs struct {
	namespace string
	command   string
	flags     []string // сырые флаги между путём и `--` (привязка — bindFlags)
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

// validateRequest сигнализирует, что запрошена валидация модуля.
type validateRequest struct {
	namespace string
}

func (e *validateRequest) Error() string { return "validate command requested" }
func (e *validateRequest) Unwrap() error { return errValidateCommand }

// helpRequest сигнализирует, что запрошена справка (последний аргумент "help").
// positional — все сегменты до "help" (namespace + command path). Диспетчер
// решает: если "<positional> help" резолвится как user-defined команда —
// выполнить её, иначе системная справка.
type helpRequest struct {
	positional []string
}

func (e *helpRequest) Error() string { return "help requested" }

// parseArgs разбирает аргументы CLI в строгом порядке: путь → флаги → `-- args`.
// Путь — подряд идущие слова от начала до первого флагового токена (начинается
// с "-" и длиннее 1 символа; одиночный "-" — обычное слово). Последнее слово
// пути = команда, остальные = namespace через ".". Токены после пути до `--` —
// сырые флаги: привязка к параметрам по схеме команды — в bindFlags.
// Всё после первого `--` = args команды.
// "list" / "validate" / "help" как последнее слово пути — системные запросы;
// у list и validate допустимы только свои флаги.
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
		return nil, errors.New("usage: futi <namespace...> <command> [--param value] [-- args] (the command path goes before flags)")
	}

	switch path[len(path)-1] {
	case "list":
		return nil, parseListRequest(path[:len(path)-1], flags)
	case "validate":
		if len(path) < 2 {
			return nil, errors.New("usage: futi <namespace...> validate")
		}
		if hasHelpFlag(flags) {
			return nil, errHelp
		}
		if len(flags) > 0 {
			return nil, fmt.Errorf("validate: unknown flag %q", flags[0])
		}
		return nil, &validateRequest{namespace: strings.Join(path[:len(path)-1], ".")}
	case "help":
		// В отличие от list/validate, help НЕ зарезервирован: диспетчер проверяет,
		// нет ли user-defined команды "help" (см. runHelp). Флаги игнорируются.
		return nil, &helpRequest{positional: path[:len(path)-1]}
	}

	// args строка: join через пробел (сохраняет разделение для шаблона)
	pa := &parsedArgs{
		flags:    flags,
		args:     strings.Join(argsList, " "),
		argsList: argsList,
	}
	if len(path) == 1 {
		// Один позиционный — namespace без команды (default cmd модуля).
		pa.namespace = path[0]
		return pa, nil
	}
	pa.namespace = strings.Join(path[:len(path)-1], ".")
	pa.command = path[len(path)-1]
	return pa, nil
}

// isFlagToken: токен — флаг, если начинается с "-" и длиннее одного символа.
func isFlagToken(tok string) bool {
	return len(tok) > 1 && tok[0] == '-'
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

// createSource создаёт источник модулей: локальный source (./.futi/ или ./futi.yaml)
// поверх глобального. global уже создан в main() и используется для cache.Start.
// loader используется для загрузки ./futi.yaml при определении имени модуля
// (если передан *modulecache.Cache — повторная загрузка того же файла в Run
// возьмёт модуль из кэша).
func createSource(loader modulerunner.ModuleLoader, global discovery.Source) (discovery.Source, error) {
	local, err := localSource(loader)
	if err != nil {
		return nil, err
	}
	if local != nil {
		return discovery.NewLayeredSource(local, global), nil
	}
	return global, nil
}

// createGlobalSource создаёт глобальный источник модулей из config.yaml
// или fallback на ~/.config/futi/modules/.
// Возвращает загруженный *config.Config (для настроек кэша) вместе с Source —
// устраняет двойную загрузку config.yaml (REVIEW #12).
// Возвращает ошибку вместо log.Fatalf — позволяет тестировать и инъектить
// ошибки (невалидный config, отсутствие home dir).
func createGlobalSource() (*config.Config, discovery.Source, error) {
	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		return nil, nil, fmt.Errorf("config path: %w", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}

	if cfg != nil && len(cfg.Sources) > 0 {
		return cfg, discovery.NewCompositeSource(cfg), nil
	}

	// Fallback: нет конфига — одиночный GlobalSource
	modDir := os.Getenv("FUTI_MODULES_DIR")
	if modDir == "" {
		modDir, err = config.DefaultModulesDir()
		if err != nil {
			return nil, nil, fmt.Errorf("modules dir: %w", err)
		}
	}
	return cfg, discovery.NewGlobalSource(modDir), nil
}

// localSource проверяет CWD на наличие локальных модулей:
//   - ./.futi/ — каталог модулей (GlobalSource)
//   - ./futi.yaml — одиночный файл модуля (FileSource, имя из поля name)
//
// Возвращает (nil, nil), если локальных модулей нет.
// При наличии обоих — ./.futi/ имеет приоритет над ./futi.yaml.
// loader загружает ./futi.yaml для извлечения name; используется abs-путь,
// чтобы ключ кэша совпадал с тем, что использует Run через FileSource.
// Возвращает ошибку при невалидном futi.yaml — автор модуля получает раннюю
// диагностику вместо непонятного "module not found" в runtime.
func localSource(loader modulerunner.ModuleLoader) (discovery.Source, error) {
	var layers []discovery.Source

	if info, err := os.Stat(".futi"); err == nil && info.IsDir() {
		abs, _ := filepath.Abs(".futi")
		layers = append(layers, discovery.NewGlobalSource(abs))
	}

	if _, err := os.Stat("futi.yaml"); err == nil {
		abs, _ := filepath.Abs("futi.yaml")
		absRoot, _ := filepath.Abs(".")
		mod, err := modulerunner.LoadModule(loader, abs, absRoot)
		if err != nil {
			return nil, fmt.Errorf("local source: %w", err)
		}
		name := mod.Name
		if name == "" {
			name = "futi"
		}
		layers = append(layers, discovery.NewFileSource(abs, absRoot, name))
	}

	if len(layers) == 0 {
		return nil, nil
	}
	if len(layers) == 1 {
		return layers[0], nil
	}
	return discovery.NewLayeredSource(layers...), nil
}

// defaultCachePath возвращает путь к gob-файлу кэша: ~/.cache/futi/modulecache.gob.
// Использует os.UserCacheDir() (поддерживает XDG_CACHE_HOME на Linux).
func defaultCachePath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache path: get cache dir: %w", err)
	}
	return filepath.Join(cacheDir, "futi", "modulecache.gob"), nil
}

// newCacheFromConfig создаёт *modulecache.Cache из конфига.
// При ошибке получения cache path — fallback на MemoryStorage (in-memory only).
// При ошибке создания policy — fallback на AlwaysPolicy с warning.
func newCacheFromConfig(cfg *config.Config) *modulecache.Cache {
	cachePath, err := defaultCachePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cache: %v (using in-memory only)\n", err)
		return modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	}
	storage := modulecache.NewDiskStorage(cachePath)

	policy := modulecache.RefreshPolicy(modulecache.AlwaysPolicy{})
	if cfg != nil {
		p, err := modulecache.NewPolicyFromConfig(cfg.Cache.Refresh, cfg.Cache.Time)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cache config: %v (using default every_time)\n", err)
		} else {
			policy = p
		}
	}
	return modulecache.New(storage, policy)
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
	result, err := cache.Refresh(globalSource)
	if err != nil {
		return fmt.Errorf("cache refresh: %w", err)
	}
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
	return nil
}

// runCacheStatus выводит текущее состояние кэша: политику, время последнего
// обновления, количество модулей, путь к gob-файлу (фикс #17).
func runCacheStatus(w io.Writer, cache *modulecache.Cache, cachePath string) error {
	fmt.Fprintf(w, "Policy: %s\n", cache.PolicyName())

	lr := cache.LastRefresh()
	if lr.IsZero() {
		fmt.Fprintln(w, "Last refresh: never")
	} else {
		fmt.Fprintf(w, "Last refresh: %s\n", lr.Format("2006-01-02 15:04:05"))
	}

	fmt.Fprintf(w, "Modules: %d\n", cache.ModuleCount())

	if cachePath == "" {
		fmt.Fprintln(w, "Storage: in-memory (no persistent cache)")
	} else {
		fmt.Fprintf(w, "Cache file: %s\n", cachePath)
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
	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cache clear: remove %s: %w", cachePath, err)
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
	// Сначала пробуем резолвить как модуль. Пустой namespace — запрос корня:
	// Resolve("") вернул бы «invalid namespace», а не ErrNotFound.
	if namespace != "" {
		manifestPath, moduleRoot, err := source.Resolve(namespace)
		if err == nil {
			return printCommands(w, manifestPath, moduleRoot, verbose, loader)
		}
		if !errors.Is(err, discovery.ErrNotFound) {
			return fmt.Errorf("list: %w", err)
		}
	}

	// Не модуль — пробуем progressive resolution для subcommands
	if namespace != "" {
		segments := strings.Split(namespace, ".")
		for i := len(segments) - 1; i > 0; i-- {
			ns := strings.Join(segments[:i], ".")
			commandPath := segments[i:]

			manifestPath, moduleRoot, err := source.Resolve(ns)
			if err != nil {
				if errors.Is(err, discovery.ErrNotFound) {
					continue
				}
				return fmt.Errorf("list: %w", err)
			}

			mod, err := modulerunner.LoadModule(loader, manifestPath, moduleRoot)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}

			cmd, err := mod.ResolveCommandPath(commandPath)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}

			if len(cmd.Commands) > 0 {
				return renderCommandTable(w, cmd.Commands, manifestPath, verbose, cmd.Cmd)
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
	return printChildren(w, children, verbose, loader)
}

func printCommands(w io.Writer, manifestPath, moduleRoot string, verbose bool, loader modulerunner.ModuleLoader) error {
	mod, err := modulerunner.LoadModule(loader, manifestPath, moduleRoot)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	return renderCommandTable(w, mod.Commands, manifestPath, verbose, mod.Cmd)
}

// runValidate загружает модуль и выводит отчёт валидации схемы.
// Использует ParseModule (без валидации) + Validate() для получения полного
// списка ошибок и предупреждений за один проход.
// Возвращает error только если есть errors (warnings не блокируют).
func runValidate(w io.Writer, source discovery.Source, namespace string) error {
	manifestPath, moduleRoot, err := source.Resolve(namespace)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	// ParseModule — raw парсинг без валидации. LoadModule здесь не подходит,
	// т.к. он падает на первой ошибке, а validate должен показать все.
	mod, err := execmodule.ParseModule(manifestPath, moduleRoot)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	errors, warnings := mod.Validate()
	report, hasErrors := execmodule.FormatValidateReport(errors, warnings)
	fmt.Fprint(w, report)

	if hasErrors {
		return fmt.Errorf("validate: module %q has %d error(s)", namespace, len(errors))
	}
	return nil
}

// renderCommandTable выводит map команд в виде отсортированной таблицы.
// Используется для top-level команд модуля и для subcommands.
// defaultCmd — если не пустой, добавляет строку (default) первой в таблице
// (для отображения default cmd модуля или fallback cmd subcommand).
func renderCommandTable(w io.Writer, commands map[string]*execmodule.Command, manifestPath string, verbose bool, defaultCmd string) error {
	if len(commands) == 0 && defaultCmd == "" {
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

	if defaultCmd != "" {
		var defaultRow []string
		if verbose {
			defaultRow = []string{"(default)", "", manifestPath}
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
	return tw.Render()
}

func printChildren(w io.Writer, children []discovery.Child, verbose bool, loader modulerunner.ModuleLoader) error {
	var rows [][]string
	for _, c := range children {
		var desc, path string
		if c.Type == "module" {
			path = c.ManifestPath
			if c.Conflict {
				// Resolve для такого имени вернёт ошибку конфликта: описание не грузим.
				// ManifestPath конфликтной записи — от x.yaml; c.Name в `list -r` —
				// полный путь (ns.x), поэтому имена файлов берём из пути.
				base := filepath.Base(c.ManifestPath)
				desc = "конфликт: " + base + " и " + strings.TrimSuffix(base, ".yaml") + "/config.yaml"
			} else if mod, err := modulerunner.LoadModule(loader, c.ManifestPath, c.ModuleRoot); err == nil {
				desc = mod.Description
			}
		}
		if verbose {
			rows = append(rows, []string{c.Type, c.Name, desc, path})
		} else {
			rows = append(rows, []string{c.Type, c.Name, desc})
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
	return tw.Render()
}

func printUsage() {
	fmt.Fprint(os.Stderr, `futi — universal CLI utility manager

Turns scattered commands, scripts and tools into YAML modules with a single
CLI interface. Install a module once — use it everywhere.

USAGE
  futi <namespace...> <command> [--param value] [--bool-flag] [-- args]
  futi <namespace>                    Invoke module's default command (if defined)
  futi <namespace...> list [flags]
  futi list [flags]
  futi <namespace...> validate        Validate module schema
  futi modules init <name> [flags]    Create a new module from template
  futi modules <subcommand>           Manage modules (install, uninstall, ...)
  futi cache refresh                  Refresh module cache
  futi completion <bash|zsh|fish>     Generate a shell completion script
  futi -h | --help

The last positional argument (without --) is the command; everything before
it is the namespace, joined with dots. Example: "futi dev deploy staging"
resolves to namespace "dev.deploy", command "staging".

The order is strict: path, then flags, then "-- args". The path ends at the
first token starting with "-"; a word after a flag is only accepted as the
value of the preceding string/int flag. "-h" / "--help" after the path shows
help for that path.

"list" and "validate" are reserved words — they cannot be used as command
names in a module.

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
  futi <ns> validate               Validate module schema (all commands and
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

  install flags:
  --local                          Install into ./.futi/ instead of the
                                   global modules directory
  --namespace <ns>                 Install under a namespace (ns/name)
  --ref <branch|tag>               Git branch or tag to install from
  --from-local/--from-git/--from-archive
                                   Force source type detection
  --skip-attach                    Skip the attach: hook

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

  Boolean values are normalized: "false", "0", "no", "" are falsy;
  "true", "1", "yes" are truthy. Use {{if .flag}}...{{end}} in templates.

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
    {{.env.FOO}}                    Environment variable FOO (error if unset)
    {{if index .env "FOO"}}...{{end}}  Conditional on env var presence
    {{if .follow}}...{{end}}        Conditional block (for bool params)

  Quote values with spaces in the template:
    cmd: "echo \"{{.msg}}\""        --msg "hello world" works correctly

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
  FUTI_MODULES_DIR                  Override modules directory (used when
                                    no config.yaml exists; default
                                    ~/.config/futi/modules/)
`)
}

// exitCodeFromError извлекает код завершения из ошибки.
// При гибели процесса от сигнала N (*exec.ExitError) — 128+N (как в shell);
// иначе, если команда прервана сигналом N, полученным futi
// (*executor.InterruptedError), — тоже 128+N; иначе ExitCode() из
// *exec.ExitError. Остальное — 1 (стандартный код ошибки для futi).
func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	hasExitErr := errors.As(err, &exitErr)
	if hasExitErr {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
	}
	var intErr *executor.InterruptedError
	if errors.As(err, &intErr) {
		return 128 + int(intErr.Signal)
	}
	if hasExitErr {
		return exitErr.ExitCode()
	}
	return 1
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
	os.Exit(exitCodeFromError(err))
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

// commandPath восстанавливает позиционный путь (сегменты namespace + команда)
// разобранной команды — тот же, что Run получает как positional.
func commandPath(pa *parsedArgs) []string {
	path := strings.Split(pa.namespace, ".")
	if pa.command != "" {
		path = append(path, pa.command)
	}
	return path
}

// exitAfterHelp печатает справку по пути в stdout (код 0); ошибка — в stderr, код 1.
func exitAfterHelp(source discovery.Source, loader modulerunner.ModuleLoader, path []string) {
	if err := runSystemHelp(os.Stdout, source, loader, path); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Быстрый путь: -h/--help не требует config/source/cache.
	if os.Args[1] == "-h" || os.Args[1] == "--help" {
		printUsage()
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
			fmt.Fprintf(os.Stderr, "error: completion requires a shell (valid: %s)\n", strings.Join(validCompletionShells, ", "))
			os.Exit(1)
		}
		if err := runCompletion(os.Stdout, os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "completion: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Создаём глобальный source и загружаем config (один раз — REVIEW #5, #12).
	cfg, globalSource, err := createGlobalSource()
	if err != nil {
		fmt.Fprintf(os.Stderr, "source: %v\n", err)
		os.Exit(1)
	}

	// Быстрый путь: futi __complete <args...> — hidden-обработчик completion.
	// ДО cache.Start: __complete сам решает, нужно ли прогревать кэш
	// (самообновление при холодном/устаревшем), и не запускает Refresh по политике
	// на каждый TAB.
	if os.Args[1] == "__complete" {
		os.Exit(runCompleteMain(os.Stdout, os.Stderr, cfg, globalSource, os.Args[2:]))
	}

	// Быстрый путь: `futi modules <subcommand>` — до cache.Start, чтобы не
	// гонять Refresh по политике: мутирующие подкоманды сами инвалидируют
	// кэш, read-only обходятся загрузкой по требованию.
	if os.Args[1] == "modules" {
		cache := newCacheFromConfig(cfg)
		source, err := createSource(cache, globalSource)
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
	// двойного Refresh (Start с AlwaysPolicy + runCacheRefresh). Для refresh:
	// cache.Load() загружает с диска без Refresh, затем runCacheRefresh —
	// единственный Refresh с корректным mtime+size сравнением.
	if subcmd, ok := cacheSubcommand(os.Args[1:]); ok {
		cache := newCacheFromConfig(cfg)
		cachePath, pathErr := defaultCachePath()
		switch subcmd {
		case "refresh":
			// Парсим --verbose/-v из оставшихся аргументов.
			verbose := false
			for _, a := range os.Args[3:] {
				if a == "-v" || a == "--verbose" {
					verbose = true
				}
			}
			cache.Load() // load from disk without Refresh
			if err := runCacheRefresh(os.Stdout, cache, globalSource, verbose); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			return
		case "status":
			cache.Load() // populate from disk for accurate module count
			if pathErr != nil {
				cachePath = ""
			}
			if err := runCacheStatus(os.Stdout, cache, cachePath); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			return
		case "clear":
			if pathErr != nil {
				cachePath = ""
			}
			if err := runCacheClear(os.Stdout, cachePath); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			return
		case "":
			fmt.Fprintln(os.Stderr, `error: cache requires a subcommand (try "cache refresh", "cache status", or "cache clear")`)
			os.Exit(1)
		default:
			fmt.Fprintf(os.Stderr, "error: unknown cache subcommand %q (try \"cache refresh\", \"cache status\", or \"cache clear\")\n", subcmd)
			os.Exit(1)
		}
	}

	// Создаём и запускаем кэш. Ошибки кэша не фатальны — CLI работает и без него.
	cache := newCacheFromConfig(cfg)
	if err := cache.Start(globalSource); err != nil {
		fmt.Fprintf(os.Stderr, "cache: %v (continuing without cache)\n", err)
	}

	pa, err := parseArgs(os.Args[1:])
	if err != nil {
		var listReq *listRequest
		var valReq *validateRequest
		var helpReq *helpRequest
		switch {
		case errors.As(err, &listReq):
			source, err := createSource(cache, globalSource)
			if err != nil {
				fmt.Fprintf(os.Stderr, "source: %v\n", err)
				os.Exit(1)
			}
			if listErr := runList(os.Stdout, source, listReq.namespace, listReq.verbose, listReq.recurse, cache); listErr != nil {
				log.Fatalf("list: %v", listErr)
			}
			return
		case errors.As(err, &valReq):
			source, err := createSource(cache, globalSource)
			if err != nil {
				fmt.Fprintf(os.Stderr, "source: %v\n", err)
				os.Exit(1)
			}
			if valErr := runValidate(os.Stdout, source, valReq.namespace); valErr != nil {
				fmt.Fprintf(os.Stderr, "%v\n", valErr)
				os.Exit(1)
			}
			return
		case errors.As(err, &helpReq):
			source, err := createSource(cache, globalSource)
			if err != nil {
				fmt.Fprintf(os.Stderr, "source: %v\n", err)
				os.Exit(1)
			}
			if helpErr := runHelp(os.Stdout, source, cache, helpReq.positional); helpErr != nil {
				fmt.Fprintf(os.Stderr, "%v\n", helpErr)
				os.Exit(1)
			}
			return
		case errors.Is(err, errHelp):
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "parse: %v\n", err)
			os.Exit(1)
		}
	}

	source, err := createSource(cache, globalSource)
	if err != nil {
		fmt.Fprintf(os.Stderr, "source: %v\n", err)
		os.Exit(1)
	}
	// Схема команды нужна для привязки флагов (bool/string/int). -h/--help —
	// справка по пути, в том числе для веток без своей команды (futi docker -h).
	helpPath := commandPath(pa)
	cmd, err := modulerunner.ResolveCommand(source, cache, pa.namespace, pa.command)
	if err != nil {
		if hasHelpFlag(pa.flags) {
			exitAfterHelp(source, cache, helpPath)
			return
		}
		fmt.Fprintf(os.Stderr, "%s\n", formatRunError(err))
		exitAfterRunError(err)
	}
	params, help, err := bindFlags(cmd, pa.flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if help {
		exitAfterHelp(source, cache, helpPath)
		return
	}
	if err := modulerunner.Run(context.Background(), source, pa.namespace, pa.command, params, pa.args, pa.argsList, nil, nil, cache); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", formatRunError(err))
		exitAfterRunError(err)
	}
}
