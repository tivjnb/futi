package modulerunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"

	"github.com/google/shlex"
)

// ModuleLoader загружает модуль из манифеста по пути.
// Реализации: *modulecache.Cache (с кэшированием) — передаётся из main.go;
// nil-fallback в тестах использует execmodule.LoadModule напрямую.
type ModuleLoader interface {
	LoadModule(manifestPath, moduleRoot string) (*execmodule.Module, error)
}

// VerifiedLoader — опциональное расширение ModuleLoader: загрузка с гарантией
// свежести/аутентичности модуля относительно файла манифеста (проверка SHA-256).
// Реализует *modulecache.Cache (LoadModuleVerified). Используется при ИСПОЛНЕНИИ
// команды (run), где отравленный/устаревший кэш мог бы привести к выполнению
// чужого кода, а также в системной справке и completion (иначе они показывают
// устаревший манифест). list использует обычный LoadModule (быстрый).
type VerifiedLoader interface {
	LoadModuleVerified(manifestPath, moduleRoot string) (*execmodule.Module, error)
}

// LoadModule вызывает loader.LoadModule, либо fallback на execmodule.LoadModule
// при nil-loader. Fallback сохраняет обратную совместимость для тестов и
// вызовов без кэша.
// Экспортирована для переиспользования в cmd/futi (фикс #35 — дедупликация).
func LoadModule(loader ModuleLoader, manifestPath, moduleRoot string) (*execmodule.Module, error) {
	if loader == nil {
		return execmodule.LoadModule(manifestPath, moduleRoot)
	}
	return loader.LoadModule(manifestPath, moduleRoot)
}

// LoadModuleVerified загружает модуль со строгой проверкой свежести/аутентичности
// (см. VerifiedLoader). Если loader реализует VerifiedLoader — использует его
// LoadModuleVerified; иначе fallback на обычный LoadModule (например, при
// nil-loader или в тестах без кэша).
func LoadModuleVerified(loader ModuleLoader, manifestPath, moduleRoot string) (*execmodule.Module, error) {
	if v, ok := loader.(VerifiedLoader); ok {
		return v.LoadModuleVerified(manifestPath, moduleRoot)
	}
	return LoadModule(loader, manifestPath, moduleRoot)
}

// Prepared — команда, готовая к исполнению.
type Prepared struct {
	Argv       []string // argv[0] — исполняемый файл
	ModuleName string
	ModuleRoot string
}

// Prepare находит модуль через source, загружает его, валидирует параметры,
// рендерит cmd-шаблон и разбирает результат в argv, ничего не исполняя.
// Поддерживает subcommands: progressive resolution пробует резолвить модуль
// от длиннейшего namespace к коротчайшему; остаток = command path.
// args — строка после `--` (для {{.args}}), argsList — массив (для {{.args_list}}).
// command="" означает вызов default cmd модуля (Module.Cmd).
// loader — опциональный загрузчик модулей с кэшем (nil = без кэша, прямой LoadModule).
func Prepare(
	source discovery.Source,
	namespace, command string,
	params map[string]string,
	args string,
	argsList []string,
	loader ModuleLoader,
) (*Prepared, error) {
	// Исполняемый модуль загружаем со строгой проверкой свежести/аутентичности
	// (SHA-256); так же — системная справка и completion. list использует
	// обычный LoadModule (быстрый).
	r, err := resolveCommand(source, namespace, command, loader)
	if err != nil {
		return nil, err
	}
	cmd, mod, moduleRoot := r.cmd, r.mod, r.moduleRoot
	moduleName := execmodule.EffectiveName(mod.Name, r.manifestPath)

	resolved, err := cmd.Validate(params)
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	// Проверка обязательного минимума позиционных аргументов (после "--").
	if err := cmd.ValidatePositional(argsList); err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	rendered, err := cmd.Render(resolved, args, argsList, execmodule.ModuleRef{Name: moduleName, Root: moduleRoot})
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	parts, err := shlex.Split(rendered)
	if err != nil {
		// Отрендеренную строку не выводим: в ней могут быть значения секретов
		// ({{.env.X}}, параметры); причина shlex сохраняется.
		return nil, fmt.Errorf("modulerunner: command %q: split rendered cmd: %w", strings.TrimSpace(namespace+" "+command), err)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("modulerunner: empty command after render")
	}

	return &Prepared{Argv: parts, ModuleName: moduleName, ModuleRoot: moduleRoot}, nil
}

// Run готовит команду через Prepare и выполняет её.
// stdout/stderr — опциональные writers для перехвата вывода (nil = os.Stdout/os.Stderr).
// stdin команды всегда наследуется от futi (os.Stdin).
// Остальные параметры — как у Prepare.
func Run(
	ctx context.Context,
	source discovery.Source,
	namespace, command string,
	params map[string]string,
	args string,
	argsList []string,
	stdout, stderr io.Writer,
	loader ModuleLoader,
) error {
	p, err := Prepare(source, namespace, command, params, args, argsList, loader)
	if err != nil {
		return err
	}

	// Unix-семантика для первого токена cmd: относительные пути резолвятся
	// ОС от cwd вызова (Go exec при Dir=="" наследует cwd родительского
	// процесса); bare-имя ищется в PATH. Свои скрипты пакета — явно через
	// {{quote .module_root}}/scripts/x.py. См. WORKDIR.md §2, §3.1.
	//
	// Env: FUTI_MODULE_NAME и FUTI_MODULE_ROOT — симметрия с хуками;
	// FUTI_CWD удалён (cwd = cwd вызова, getcwd() в скрипте даёт то же).
	env := os.Environ()
	env = append(env,
		"FUTI_MODULE_NAME="+p.ModuleName,
		"FUTI_MODULE_ROOT="+p.ModuleRoot,
	)
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cfg := &executor.Config{
		Env:            env,
		Stdin:          os.Stdin,
		Stdout:         stdout,
		Stderr:         stderr,
		ForwardSignals: true,
		Group:          executor.GroupInherit,
	}

	return executor.RunWithConfig(ctx, cfg, p.Argv[0], p.Argv[1:]...)
}

// resolvedCommand — результат резолва команды: сама команда, модуль и корень модуля.
type resolvedCommand struct {
	cmd        *execmodule.Command
	mod        *execmodule.Module
	moduleRoot string
	// manifestPath нужен для имени модуля по умолчанию (execmodule.EffectiveName).
	manifestPath string
}

// ResolveCommand находит модуль и команду (progressive resolution, как в Run)
// и возвращает схему команды: params/positional. command="" — default cmd
// модуля. Схема нужна для исполнения, поэтому модуль грузится так же, как в Run —
// через LoadModuleVerified (свежесть относительно манифеста на диске); иначе
// привязка флагов могла бы разойтись со схемой, по которой Run валидирует.
// Ошибки те же, что у Run до валидации параметров.
func ResolveCommand(source discovery.Source, loader ModuleLoader, namespace, command string) (*execmodule.Command, error) {
	r, err := resolveCommand(source, namespace, command, loader)
	if err != nil {
		return nil, err
	}
	return r.cmd, nil
}

// ErrModuleNotFound — ни один префикс пути не резолвится в модуль.
var ErrModuleNotFound = errors.New("module not found")

// resolveCommand — общий progressive resolution для Run и ResolveCommand:
// пробует резолвить модуль от длиннейшего namespace к коротчайшему, остаток =
// command path. Модуль грузится через LoadModuleVerified.
func resolveCommand(
	source discovery.Source,
	namespace, command string,
	loader ModuleLoader,
) (*resolvedCommand, error) {
	// Реконструкция positional args из namespace + command.
	// Непустой namespace всегда делится по ".", в т.ч. при command=="":
	// `futi docker.build` даёт namespace "docker.build" без команды.
	var positional []string
	if namespace != "" {
		positional = strings.Split(namespace, ".")
	}
	if namespace == "" || command != "" {
		positional = append(positional, command)
	}

	// i=len(positional) → ns=все, commandPath=[] (default cmd случай).
	// i>=1: пустой namespace (i=0) модулем не является.
	for i := len(positional); i >= 1; i-- {
		ns := strings.Join(positional[:i], ".")
		commandPath := positional[i:]

		manifestPath, moduleRoot, err := source.Resolve(ns)
		if err != nil {
			if errors.Is(err, discovery.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("modulerunner: %w", err)
		}

		mod, err := LoadModuleVerified(loader, manifestPath, moduleRoot)
		if err != nil {
			return nil, fmt.Errorf("modulerunner: %w", err)
		}

		// commandPath пустой → default cmd модуля (со схемой: params/positional).
		if len(commandPath) == 0 {
			if mod.Cmd == "" {
				return nil, fmt.Errorf("modulerunner: module %q has no default command; specify one", ns)
			}
			return &resolvedCommand{
				cmd: &execmodule.Command{
					Cmd:        mod.Cmd,
					Params:     mod.Params,
					Positional: mod.Positional,
				},
				mod:          mod,
				moduleRoot:   moduleRoot,
				manifestPath: manifestPath,
			}, nil
		}
		cmd, err := mod.ResolveCommand(commandPath)
		if err != nil {
			return nil, fmt.Errorf("modulerunner: %w", err)
		}
		return &resolvedCommand{cmd: cmd, mod: mod, moduleRoot: moduleRoot, manifestPath: manifestPath}, nil
	}

	return nil, fmt.Errorf("modulerunner: %w for %v", ErrModuleNotFound, positional)
}

// CommandInfo описывает команду модуля для листинга.
type CommandInfo struct {
	Name        string
	Description string
}

// ListCommands находит модуль через source, загружает его и возвращает
// список команд с описаниями, отсортированный по имени.
// loader — опциональный загрузчик с кэшем (nil = прямой LoadModule).
func ListCommands(source discovery.Source, namespace string, loader ModuleLoader) ([]CommandInfo, error) {
	manifestPath, moduleRoot, err := source.Resolve(namespace)
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	mod, err := LoadModule(loader, manifestPath, moduleRoot)
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	cmds := make([]CommandInfo, 0, len(mod.Commands))
	for name, cmd := range mod.Commands {
		cmds = append(cmds, CommandInfo{
			Name:        name,
			Description: cmd.Description,
		})
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds, nil
}
