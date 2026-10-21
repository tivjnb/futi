package modulerunner

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"

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

// Env возвращает окружение процесса команды: os.Environ() плюс FUTI_MODULE_NAME
// и FUTI_MODULE_ROOT (симметрия с хуками; FUTI_CWD удалён — cwd = cwd вызова).
func (p *Prepared) Env() []string {
	env := os.Environ()
	return append(env,
		"FUTI_MODULE_NAME="+p.ModuleName,
		"FUTI_MODULE_ROOT="+p.ModuleRoot,
	)
}

// Resolved — найденная команда модуля: сама команда, модуль и его расположение.
// Модуль загружается один раз в Resolve; Validate и Prepare его не перечитывают.
type Resolved struct {
	Command      *execmodule.Command
	Module       *execmodule.Module
	ModuleRoot   string
	ManifestPath string // нужен для имени модуля по умолчанию (execmodule.EffectiveName)

	// label — namespace + command для текста ошибки shlex.
	label string
}

// ModuleName — эффективное имя модуля.
func (r *Resolved) ModuleName() string {
	return execmodule.EffectiveName(r.Module.Name, r.ManifestPath)
}

// Validate проверяет параметры и минимум позиционных аргументов (после "--")
// и возвращает значения параметров с применёнными значениями по умолчанию.
func (r *Resolved) Validate(params map[string]string, argsList []string) (map[string]string, error) {
	resolved, err := r.Command.Validate(params)
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}
	if err := r.Command.ValidatePositional(argsList); err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}
	return resolved, nil
}

// Prepare рендерит cmd-шаблон и разбирает результат в argv, ничего не исполняя.
// {{.args}} — argsList, склеенный через пробел; {{.args_list}} — сам массив.
func (r *Resolved) Prepare(resolved map[string]string, argsList []string) (*Prepared, error) {
	moduleName := r.ModuleName()
	rendered, err := r.Command.Render(resolved, strings.Join(argsList, " "), argsList,
		execmodule.ModuleRef{Name: moduleName, Root: r.ModuleRoot})
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}

	parts, err := shlex.Split(rendered)
	if err != nil {
		// Отрендеренную строку не выводим: в ней могут быть значения секретов
		// ({{.env.X}}, параметры); причина shlex сохраняется.
		return nil, fmt.Errorf("modulerunner: command %q: split rendered cmd: %w", r.label, err)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("modulerunner: empty command after render")
	}

	return &Prepared{Argv: parts, ModuleName: moduleName, ModuleRoot: r.ModuleRoot}, nil
}

// ErrModuleNotFound — ни один префикс пути не резолвится в модуль.
var ErrModuleNotFound = errors.New("module not found")

// ErrNoDefaultCommand — модуль найден, но у него нет default cmd, а команда не указана.
var ErrNoDefaultCommand = errors.New("has no default command; specify one")

// Resolve — progressive resolution: пробует резолвить модуль от длиннейшего
// namespace к коротчайшему, остаток = command path. command="" — default cmd
// модуля. Модуль грузится через LoadModuleVerified (строгая проверка
// свежести/аутентичности, как при исполнении).
func Resolve(
	source discovery.Source,
	loader ModuleLoader,
	namespace, command string,
) (*Resolved, error) {
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

	label := strings.TrimSpace(namespace + " " + command)

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
				return nil, fmt.Errorf("modulerunner: module %q %w", ns, ErrNoDefaultCommand)
			}
			return &Resolved{
				Command: &execmodule.Command{
					Cmd:        mod.Cmd,
					Params:     mod.Params,
					Positional: mod.Positional,
				},
				Module:       mod,
				ModuleRoot:   moduleRoot,
				ManifestPath: manifestPath,
				label:        label,
			}, nil
		}
		cmd, err := mod.ResolveCommand(commandPath)
		if err != nil {
			return nil, fmt.Errorf("modulerunner: %w", err)
		}
		return &Resolved{Command: cmd, Module: mod, ModuleRoot: moduleRoot, ManifestPath: manifestPath, label: label}, nil
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
