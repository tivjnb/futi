// Package completion — логика автодополнения futi, не зависящая от I/O.
//
// Complete — чистая функция: принимает source (discovery), loader (кэш или nil)
// и текущую позицию ввода (args + toComplete), возвращает подсказки и директиву.
// Вызывается из hidden-команды `futi __complete` (см. cmd/futi/completion.go).
package completion

import (
	"errors"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// Match — одна подсказка автодополнения. Description опционален и выводится
// в zsh (compadd -d); bash/fish в MVP его игнорируют.
type Match struct {
	Text        string
	Description string
}

// ReservedNames — единый canonical-список слов корня, которые нельзя
// использовать ни в одном сегменте пути модуля: main.go перехватывает их
// до резолва модулей (cache/modules/completion/__complete — по первому
// аргументу, list — по последнему позиционному), поэтому модуль с таким
// сегментом никогда не будет вызван. При добавлении новой builtin-команды
// в main.go — добавить имя сюда. help не зарезервирован: диспетчер сначала
// пробует его как пользовательскую команду.
var ReservedNames = []string{"list", "cache", "modules", "completion", "__complete"}

// hiddenReserved — зарезервированное слово, которое не подсказывается на корне.
const hiddenReserved = "__complete"

// rootBuiltins — builtin-подсказки корня: ReservedNames без скрытой
// __complete. Модуль с таким именем на корне не подсказывается: CLI
// исполнит builtin.
func rootBuiltins() []string {
	out := make([]string, 0, len(ReservedNames))
	for _, r := range ReservedNames {
		if r != hiddenReserved {
			out = append(out, r)
		}
	}
	return out
}

// IsReservedName сообщает, входит ли name в ReservedNames.
func IsReservedName(name string) bool {
	for _, r := range ReservedNames {
		if r == name {
			return true
		}
	}
	return false
}

// cacheSubcommands — подкоманды `futi cache`.
var cacheSubcommands = []string{"clear", "refresh", "status"}

// completionShells — поддерживаемые shell для `futi completion`.
var completionShells = []string{"bash", "fish", "zsh"}

// modulesSubcommands — подкоманды `futi modules`.
var modulesSubcommands = []string{"add_command", "info", "init", "install", "list", "path", "remove_command", "uninstall", "update", "validate"}

// modulesAddCommandFlags — флаги `futi modules add_command`.
var modulesAddCommandFlags = []string{"--cmd", "--desc"}

// ModulesFlags возвращает флаги подкоманды `futi modules <sub>`, которые
// подсказывает completion (nil — у подкоманды флагов нет). Экспортируется
// для теста согласованности с парсерами в cmd/futi.
func ModulesFlags(sub string) []string {
	switch sub {
	case "add_command":
		return modulesAddCommandFlags
	case "init":
		return modulesInitFlags
	case "install":
		return modulesInstallFlags
	case "uninstall":
		return modulesUninstallFlags
	case "update":
		return modulesUpdateFlags
	}
	return nil
}

// modulesInitFlags — флаги `futi modules init` (бывший futi init).
var modulesInitFlags = []string{"--command", "--default-cmd", "--desc", "--global"}

// modulesInstallFlags — флаги `futi modules install`.
var modulesInstallFlags = []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--ref", "--skip-attach", "--yes"}

// modulesUninstallFlags — флаги `futi modules uninstall`.
var modulesUninstallFlags = []string{"--force", "--skip-detach", "--strict-detach", "--yes"}

// modulesUpdateFlags — флаги `futi modules update`.
var modulesUpdateFlags = []string{"--skip-attach", "--yes"}

// helpKeyword — системная команда справки, подсказывается на уровнях
// модуля/команды/subcommand. help НЕ зарезервирован: если в модуле есть
// команда "help", она уже входит в commandMatches; здесь добавляем системную
// только если её нет.
const helpKeyword = "help"

// listKeyword — системная команда списка, подсказывается там, где
// `futi <path> list` выполняется успешно: namespace, модуль, ветка с
// subcommands. На листовой команде list даёт ошибку — не подсказываем.
// Имя "list" в командах модуля запрещено схемой, но дубль всё равно исключаем.
const listKeyword = "list"

// Complete возвращает подсказки для заданной позиции ввода.
//
// Вход:
//   - source — глобальный + локальный (layered) источник модулей;
//   - loader — загрузчик модулей с кэшем (обычно *modulecache.Cache), nil допустим;
//   - args — позиционные аргументы без `futi` и без toComplete (уже введённые слова);
//   - toComplete — текущее слово под курсором (может быть "").
//
// Логика повторяет progressive resolution из modulerunner.Run: модуль ищется
// от длиннейшего namespace к коротчайшему, остаток трактуется как command path.
func Complete(source discovery.Source, loader modulerunner.ModuleLoader, args []string, toComplete string) ([]Match, Directive) {
	// После `--` начинаются args команды — их значения user-supplied,
	// подсказывать нечего. Разрешаем shell дополнять файлы (Default).
	for _, a := range args {
		if a == "--" {
			return nil, ShellCompDirectiveDefault
		}
	}

	// Путь (namespace + command) — слова от начала до первого флагового токена,
	// как в parseArgs (cmd/futi/main.go): флаг — токен с "-" и длиннее 1 символа.
	// Всё после первого флага — флаги и их значения, не слова пути.
	positionals := args
	var flagArgs []string
	for i, a := range args {
		if len(a) > 1 && a[0] == '-' {
			positionals, flagArgs = args[:i], args[i:]
			break
		}
	}

	// Резолвим модуль progressive-ом. commandPath — остаток после namespace.
	// Зарезервированное первое слово — builtin: CLI исполнит его до резолва
	// модулей, поэтому одноимённый модуль не подсказываем.
	var (
		manifest, root string
		moduleFound    bool
		commandPath    []string
	)
	if len(positionals) == 0 || !IsReservedName(positionals[0]) {
		manifest, root, moduleFound, commandPath = resolveModule(source, positionals)
	}

	// Случай: предыдущее слово — флаг без "=", а текущее — не флаг. Bool-флаг
	// значения не берёт (парсер: "--force true" — ошибка), поэтому после него
	// подсказываем оставшиеся флаги (ниже). Значение string/int-флага
	// user-supplied: разрешаем шеллу файловое дополнение (пути в значениях).
	// В будущем ограничим параметрами типа path (ROADMAP в README).
	if len(args) > 0 && strings.HasPrefix(args[len(args)-1], "--") && !strings.Contains(args[len(args)-1], "=") && !strings.HasPrefix(toComplete, "--") {
		if !moduleFound || !isBoolFlag(loader, manifest, root, commandPath, flagName(args[len(args)-1])) {
			return nil, ShellCompDirectiveDefault
		}
	}

	// Случай: toComplete начинается с "--" — подсказываем имена параметров.
	if strings.HasPrefix(toComplete, "--") {
		// `--param=val` — значение user-supplied: разрешаем файлы (пути).
		if strings.Contains(toComplete, "=") {
			return nil, ShellCompDirectiveDefault
		}
		if moduleFound {
			if cmd, err := currentCommand(loader, manifest, root, commandPath); err == nil {
				return filterMatches(withoutUsed(paramMatches(cmd, toComplete), flagArgs), toComplete), ShellCompDirectiveNoFileComp
			}
		}
		// Флаги `futi modules <sub>` (модуль не задействован).
		if len(positionals) >= 2 && positionals[0] == "modules" {
			if flags := ModulesFlags(positionals[1]); flags != nil {
				return filterMatches(withoutUsed(flagMatches(flags, toComplete), flagArgs), toComplete), ShellCompDirectiveNoFileComp
			}
		}
		return nil, ShellCompDirectiveNoFileComp
	}

	// После первого флага слова пути невозможны: только оставшиеся флаги
	// листовой команды (без help — после флагов это уже не команда).
	if len(flagArgs) > 0 {
		if moduleFound {
			if cmd, err := currentCommand(loader, manifest, root, commandPath); err == nil && len(cmd.Commands) == 0 {
				return filterMatches(withoutUsed(paramMatches(cmd, ""), flagArgs), toComplete), ShellCompDirectiveNoFileComp
			}
		}
		return nil, ShellCompDirectiveNoFileComp
	}

	// Случай: модуль зарезолвен — подсказываем команды/subcommands.
	if moduleFound {
		return moduleMatches(loader, manifest, root, commandPath, toComplete)
	}

	// Модуль не зарезолвен — подсказываем children namespace и/или builtins.
	return namespaceMatches(source, loader, positionals, toComplete)
}

// withoutUsed убирает из подсказок параметры, уже введённые как --name или
// --name=val (повтор флага парсер допускает, но подсказывать его незачем).
func withoutUsed(matches []Match, flagArgs []string) []Match {
	used := make(map[string]bool)
	for _, a := range flagArgs {
		if strings.HasPrefix(a, "--") {
			used["--"+flagName(a)] = true
		}
	}
	out := make([]Match, 0, len(matches))
	for _, m := range matches {
		if !used[m.Text] {
			out = append(out, m)
		}
	}
	return out
}

// isBoolFlag сообщает, что name — bool-параметр текущей команды.
func isBoolFlag(loader modulerunner.ModuleLoader, manifest, root string, commandPath []string, name string) bool {
	cmd, err := currentCommand(loader, manifest, root, commandPath)
	if err != nil {
		return false
	}
	for _, p := range cmd.Params {
		if p.Name == name && p.Type == "bool" {
			return true
		}
	}
	return false
}

// resolveModule ищет модуль progressive-ом от длиннейшего namespace к коротчайшему.
// Возвращает путь манифеста, корень, флаг найденности и оставшийся command path.
func resolveModule(source discovery.Source, positionals []string) (manifest, root string, found bool, commandPath []string) {
	for i := len(positionals); i >= 0; i-- {
		ns := strings.Join(positionals[:i], ".")
		mp, mr, err := source.Resolve(ns)
		if err == nil {
			return mp, mr, true, positionals[i:]
		}
	}
	return "", "", false, nil
}

// trimCommandPath обрезает commandPath до первой неизвестной команды.
// Слова после флагов (их значения, например "--out file.png" → "file.png")
// не должны резолвиться как subcommands — они отсекаются.
func trimCommandPath(mod *execmodule.Module, commandPath []string) []string {
	level := mod.Commands
	known := 0
	for _, seg := range commandPath {
		next, ok := level[seg]
		if !ok {
			break
		}
		known++
		level = next.Commands
	}
	return commandPath[:known]
}

// currentCommand загружает модуль и возвращает команду по commandPath.
// При пустом commandPath возвращает default-команду модуля (cmd + params),
// если она есть (модуль-однокоманда); иначе notFoundError.
func currentCommand(loader modulerunner.ModuleLoader, manifest, root string, commandPath []string) (*execmodule.Command, error) {
	mod, err := modulerunner.LoadModule(loader, manifest, root)
	if err != nil {
		return nil, err
	}
	commandPath = trimCommandPath(mod, commandPath)
	if len(commandPath) == 0 {
		if mod.Cmd == "" {
			return nil, &notFoundError{"module has no default command"}
		}
		return &execmodule.Command{
			Cmd:        mod.Cmd,
			Params:     mod.Params,
			Positional: mod.Positional,
		}, nil
	}
	return mod.ResolveCommandPath(commandPath)
}

// notFoundError — вспомогательная ошибка "нечего подсказывать".
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string { return e.msg }

// moduleMatches формирует подсказки для зарезолвленного модуля.
func moduleMatches(loader modulerunner.ModuleLoader, manifest, root string, commandPath []string, toComplete string) ([]Match, Directive) {
	mod, err := modulerunner.LoadModule(loader, manifest, root)
	if err != nil {
		// Невалидный/незагружаемый модуль — не падаем, просто ничего не подсказываем
		// (консистентно с runList через ParseModule).
		return nil, ShellCompDirectiveNoFileComp
	}

	// Обрезаем commandPath до первой неизвестной команды: значения флагов
	// ("--out file.png") не резолвятся как subcommands.
	commandPath = trimCommandPath(mod, commandPath)

	// commandPath пуст → подсказываем команды модуля (+ системную help).
	// Модуль-однокоманда (без commands, с default cmd) — листовая:
	// подсказываем параметры default-команды.
	if len(commandPath) == 0 {
		if len(mod.Commands) == 0 && mod.Cmd != "" {
			return filterMatches(withList(withHelp(paramMatches(&execmodule.Command{Params: mod.Params}, ""))), toComplete), ShellCompDirectiveNoFileComp
		}
		return filterMatches(withList(withHelp(commandMatches(mod.Commands))), toComplete), ShellCompDirectiveNoFileComp
	}

	cmd, err := mod.ResolveCommandPath(commandPath)
	if err != nil {
		// Команда не найдена или путь оборван — дальше не спускаемся.
		return nil, ShellCompDirectiveNoFileComp
	}

	// Ветка с subcommands → подсказываем их (+ системную help).
	if len(cmd.Commands) > 0 {
		return filterMatches(withList(withHelp(commandMatches(cmd.Commands))), toComplete), ShellCompDirectiveNoFileComp
	}

	// Листовая команда — подсказываем все параметры (--name) даже без
	// ввода "--", плюс системную help. Пометки "(required)" / "(default: X)"
	// выводятся в zsh; в bash/fish не загрязняют ввод.
	return filterMatches(withHelp(paramMatches(cmd, "")), toComplete), ShellCompDirectiveNoFileComp
}

// withHelp добавляет системную подсказку "help" к matches, если там ещё нет
// команды с именем "help" (user-defined help уже в списке — не дублируем).
func withHelp(matches []Match) []Match {
	for _, m := range matches {
		if m.Text == helpKeyword {
			return matches
		}
	}
	return append(matches, Match{Text: helpKeyword, Description: "Show help"})
}

// withList добавляет системную подсказку "list" (без дубля).
func withList(matches []Match) []Match {
	for _, m := range matches {
		if m.Text == listKeyword {
			return matches
		}
	}
	return append(matches, Match{Text: listKeyword, Description: "List commands"})
}

// namespaceMatches формирует подсказки, когда модуль не зарезолвлен:
// builtins (корень) + children namespace.
func namespaceMatches(source discovery.Source, loader modulerunner.ModuleLoader, positionals []string, toComplete string) ([]Match, Directive) {
	// Builtins корня с собственными подсказками. Остальные зарезервированные
	// слова (list, __complete) и позиции глубже — подсказывать нечего.
	if len(positionals) > 0 && IsReservedName(positionals[0]) {
		var names []string
		switch {
		case len(positionals) == 1 && positionals[0] == "cache":
			names = cacheSubcommands
		case len(positionals) == 1 && positionals[0] == "completion":
			names = completionShells
		case len(positionals) == 1 && positionals[0] == "modules":
			names = modulesSubcommands
		case len(positionals) == 2 && positionals[0] == "modules" && positionals[1] == "validate":
			return validateMatches(source, toComplete), ShellCompDirectiveNoFileComp
		}
		return filterMatches(strMatches(names), toComplete), ShellCompDirectiveNoFileComp
	}

	ns := strings.Join(positionals, ".")
	var matches []Match

	// Builtins только на корне (positionals пуст).
	if len(positionals) == 0 {
		for _, b := range rootBuiltins() {
			matches = append(matches, Match{Text: b})
		}
	}

	// Children namespace. Ошибку (несуществующий namespace) проглатываем —
	// возвращаем пустой список, как и положено по протоколу. Частичный листинг
	// (часть sources нечитаема) — валидный результат: подсказки молча из читаемых.
	children, err := source.ListChildren(ns)
	var partial *discovery.PartialError
	if err == nil || errors.As(err, &partial) {
		for _, c := range children {
			// На корне имя builtin выигрывает у модуля/namespace: без дубля.
			if len(positionals) == 0 && IsReservedName(c.Name) {
				continue
			}
			m := Match{Text: c.Name}
			if c.Type == "module" && !c.Conflict { // конфликтный модуль не загружаем
				// Подтягиваем описание модуля (zsh); ошибку игнорируем.
				if mod, err := modulerunner.LoadModule(loader, c.ManifestPath, c.ModuleRoot); err == nil {
					m.Description = mod.Description
				}
			}
			matches = append(matches, m)
		}
		// Существующий namespace ниже корня: `futi <ns> list` работает.
		// На корне list уже среди builtins.
		if len(positionals) > 0 {
			matches = withList(matches)
		}
	}

	return filterMatches(matches, toComplete), ShellCompDirectiveNoFileComp
}

// validateMatches — полные точечные имена модулей для `futi modules validate`.
// Частичный листинг (*PartialError) — подсказки из читаемых источников.
func validateMatches(source discovery.Source, toComplete string) []Match {
	mods, err := source.List()
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return nil
	}
	matches := make([]Match, 0, len(mods))
	for _, m := range mods {
		matches = append(matches, Match{Text: m.Name})
	}
	return filterMatches(matches, toComplete)
}

// commandMatches превращает map команд в подсказки (имя + описание).
func commandMatches(commands map[string]*execmodule.Command) []Match {
	matches := make([]Match, 0, len(commands))
	for name, cmd := range commands {
		matches = append(matches, Match{Text: name, Description: cmd.Description})
	}
	return matches
}

// strMatches превращает список строк в подсказки.
func strMatches(names []string) []Match {
	matches := make([]Match, 0, len(names))
	for _, n := range names {
		matches = append(matches, Match{Text: n})
	}
	return matches
}

// flagMatches отбирает флаги по префиксу (без "--").
func flagMatches(flags []string, toComplete string) []Match {
	prefix := strings.TrimPrefix(toComplete, "--")
	matches := make([]Match, 0)
	for _, f := range flags {
		if strings.HasPrefix(strings.TrimPrefix(f, "--"), prefix) {
			matches = append(matches, Match{Text: f})
		}
	}
	return matches
}

// paramMatches отбирает параметры команды по префиксу toComplete (с "--").
// Каждый параметр несёт пометку: "(required)" или "(default: X)" —
// zsh показывает её рядом с флагом, bash/fish отрезают при выводе.
func paramMatches(cmd *execmodule.Command, toComplete string) []Match {
	prefix := strings.TrimPrefix(toComplete, "--")
	matches := make([]Match, 0)
	for _, p := range cmd.Params {
		if strings.HasPrefix(p.Name, prefix) {
			matches = append(matches, Match{Text: "--" + p.Name, Description: paramHint(p)})
		}
	}
	return matches
}

// paramHint формирует компактную пометку параметра для подсказки.
func paramHint(p execmodule.Param) string {
	if p.Required {
		return "(required)"
	}
	if p.Default != "" {
		return "(default: " + p.Default + ")"
	}
	return ""
}

// filterMatches оставляет подсказки, чьи имена начинаются с toComplete
// (case-sensitive префикс), и сортирует по алфавиту. Повторы Text
// (модуль x и namespace x) сводятся в одну подсказку; сохраняется
// непустое описание (описание модуля).
func filterMatches(matches []Match, toComplete string) []Match {
	var out []Match
	index := make(map[string]int)
	for _, m := range matches {
		if !strings.HasPrefix(m.Text, toComplete) {
			continue
		}
		if i, ok := index[m.Text]; ok {
			if out[i].Description == "" {
				out[i].Description = m.Description
			}
			continue
		}
		index[m.Text] = len(out)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Text < out[j].Text
	})
	return out
}

// flagName извлекает имя флага из "--name" или "--name=value".
func flagName(arg string) string {
	name := strings.TrimPrefix(arg, "--")
	if idx := strings.Index(name, "="); idx >= 0 {
		return name[:idx]
	}
	return name
}
