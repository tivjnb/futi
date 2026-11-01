package modulerunner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/inputs"
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
	Args       []argvtmpl.Arg
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
// Модуль загружается один раз в ResolveWords; Validate и Prepare его не перечитывают.
type Resolved struct {
	Command      *execmodule.Command
	Module       *execmodule.Module
	ModuleRoot   string
	ManifestPath string // нужен для имени модуля по умолчанию (execmodule.EffectiveName)
}

// ModuleName — эффективное имя модуля.
func (r *Resolved) ModuleName() string {
	return execmodule.EffectiveName(r.Module.Name, r.ManifestPath)
}

// Validate binds the given inputs (string, bool, []string or []any of
// strings, keyed by input name) to the schema of the command and returns the
// values for the template, with defaults applied.
func (r *Resolved) Validate(given map[string]any) (map[string]argvtmpl.Value, error) {
	return inputs.Bind(r.Command.Args, r.Command.Flags, given)
}

// Prepare evaluates the cmd template to argv, without running anything, and
// applies the dash guard: an element starting with "-" whose first character
// comes from an input value is a *argvtmpl.DashError.
func (r *Resolved) Prepare(vals map[string]argvtmpl.Value) (*Prepared, error) {
	tmpl, err := r.Command.Cmd.Template()
	if err != nil {
		return nil, fmt.Errorf("modulerunner: cmd: %w", err)
	}
	specs := inputs.TemplateInputs(r.Command.Args, r.Command.Flags)
	moduleName := r.ModuleName()
	args, err := tmpl.Eval(specs, argvtmpl.Values{Inputs: vals, ModuleName: moduleName, ModuleRoot: r.ModuleRoot})
	if err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("modulerunner: command rendered to empty argv")
	}
	argv := argvtmpl.Argv(args)
	if err := argvtmpl.CheckDash(args, specs, filepath.Base(argv[0])); err != nil {
		return nil, fmt.Errorf("modulerunner: %w", err)
	}
	return &Prepared{Argv: argv, Args: args, ModuleName: moduleName, ModuleRoot: r.ModuleRoot}, nil
}

// ErrModuleNotFound is the class of an unknown first word of a command path.
var ErrModuleNotFound = errors.New("module not found")

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
