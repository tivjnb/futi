// Package completion — логика автодополнения futi, не зависящая от I/O.
//
// Complete — чистая функция: принимает source (discovery), loader (кэш или nil)
// и текущую позицию ввода (args + toComplete), возвращает подсказки и директиву.
// Вызывается из hidden-команды `futi __complete` (см. cmd/futi/completion.go).
package completion

import (
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/inputs"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// Match — одна подсказка автодополнения. Description опционален и выводится
// в zsh (compadd -d); bash/fish в MVP его игнорируют.
type Match struct {
	Text        string
	Description string
}

// ReservedNames is the canonical list of builtin commands. The CLI recognizes
// them only as the first word, so only top-level names (root modules and
// namespaces) are reserved; a command help or list inside a module is fine.
// Add the name here when adding a builtin command to the CLI.
var ReservedNames = []string{"help", "list", "cache", "modules", "completion", "__complete"}

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

// globalFlags — глобальные флаги futi, подсказываются на позиции 0.
var globalFlags = []string{"--dry-run", "--help", "--version"}

// cacheSubcommands — подкоманды `futi cache`.
var cacheSubcommands = []string{"clear", "refresh", "status"}

// completionShells — поддерживаемые shell для `futi completion`.
var completionShells = []string{"bash", "fish", "zsh"}

// modulesSubcommands — подкоманды `futi modules`.
var modulesSubcommands = []string{"add_command", "info", "init", "install", "list", "path", "remove_command", "uninstall", "update", "validate"}

// ModulesSubcommands возвращает подкоманды `futi modules`, которые подсказывает
// completion (для теста согласованности с парсерами в cmd/futi).
func ModulesSubcommands() []string {
	return append([]string(nil), modulesSubcommands...)
}

// modulesAddCommandFlags — флаги `futi modules add_command`.
var modulesAddCommandFlags = []string{"--cmd", "--desc"}

// ModulesFlags возвращает флаги подкоманды `futi modules <sub>`, которые
// подсказывает completion (nil — у подкоманды флагов нет). Экспортируется
// для теста согласованности с парсерами в cmd/futi.
func ModulesFlags(sub string) []string {
	switch sub {
	case "add_command":
		return append([]string(nil), modulesAddCommandFlags...)
	case "init":
		return append([]string(nil), modulesInitFlags...)
	case "install":
		return append([]string(nil), modulesInstallFlags...)
	case "uninstall":
		return append([]string(nil), modulesUninstallFlags...)
	case "update":
		return append([]string(nil), modulesUpdateFlags...)
	}
	return nil
}

// modulesValueFlags — флаги `futi modules <sub>`, берущие значение (остальные
// флаги подкоманды булевы).
var modulesValueFlags = map[string][]string{
	"add_command": {"--cmd", "--desc"},
	"init":        {"--command", "--default-cmd", "--desc"},
	"install":     {"--namespace", "--ref"},
}

// ModulesValueFlags возвращает флаги подкоманды `futi modules <sub>`, берущие
// значение. Экспортируется для теста согласованности с парсерами в cmd/futi.
func ModulesValueFlags(sub string) []string {
	return append([]string(nil), modulesValueFlags[sub]...)
}

// modulesInitFlags — флаги `futi modules init` (бывший futi init).
var modulesInitFlags = []string{"--command", "--default-cmd", "--desc", "--global"}

// modulesInstallFlags — флаги `futi modules install`.
var modulesInstallFlags = []string{"--from-archive", "--from-git", "--from-local", "--local", "--namespace", "--ref", "--skip-attach", "--yes"}

// modulesUninstallFlags — флаги `futi modules uninstall`.
var modulesUninstallFlags = []string{"--force", "--skip-detach", "--strict-detach", "--yes"}

// modulesUpdateFlags — флаги `futi modules update`.
var modulesUpdateFlags = []string{"--skip-attach", "--yes"}

// helpKeyword and listKeyword are the builtin commands whose arguments are
// command paths: `futi help <path>`, `futi list <path>`.
const (
	helpKeyword = "help"
	listKeyword = "list"
)

// Complete returns suggestions for the given input position.
//
// Input:
//   - source — the layered (global + local) module source;
//   - loader — module loader with a cache (usually *modulecache.Cache), may be nil;
//   - args — the words already typed, without `futi` and without toComplete;
//   - toComplete — the word under the cursor (may be "").
//
// It follows the grammar of the command line: the command path is walked over
// the merged tree (modulerunner.Walk), the words after it are scanned like the
// real parser does (inputs.Scan). A module that cannot be loaded or a path
// collision give no suggestions.
func Complete(source discovery.Source, loader modulerunner.ModuleLoader, args []string, toComplete string) ([]Match, Directive) {
	// A leading global --dry-run is not a part of the path.
	dryRunGiven := len(args) > 0 && args[0] == "--dry-run"
	if dryRunGiven {
		args = args[1:]
	}

	// At position 0 a word with "-" is a global flag of futi.
	if len(args) == 0 && strings.HasPrefix(toComplete, "-") {
		flags := globalFlags
		if dryRunGiven {
			// --dry-run is already typed, and --help/--version cannot go with it.
			flags = nil
		}
		return filterMatches(strMatches(flags), toComplete), ShellCompDirectiveNoFileComp
	}

	// The first reserved word is a builtin: the CLI runs it before resolving
	// modules, so a module of the same name is not suggested. With --dry-run
	// builtins are not allowed at all.
	if len(args) > 0 && IsReservedName(args[0]) {
		switch {
		case dryRunGiven:
			return nil, ShellCompDirectiveNoFileComp
		case args[0] == helpKeyword || args[0] == listKeyword:
			return helpPathMatches(source, loader, args[1:], toComplete)
		}
		return builtinMatches(source, args, toComplete)
	}

	return pathMatches(source, loader, args, toComplete, dryRunGiven)
}

// pathMatches suggests what can follow args: the flags of the node reached by
// the path, its subcommands or nothing.
func pathMatches(source discovery.Source, loader modulerunner.ModuleLoader, args []string, toComplete string, dryRun bool) ([]Match, Directive) {
	// Verified loading: a stale cache entry must not hide the current flags
	// and subcommands of a module (TestComplete*ReloadsChangedManifest).
	node, n, err := modulerunner.Walk(source, loader, args, modulerunner.WalkOptions{Verified: true})
	if err != nil || (n == 0 && len(args) > 0) {
		return nil, ShellCompDirectiveNoFileComp
	}
	rest := args[n:]

	var argSpecs, flagSpecs []inputs.Spec
	if node.Command != nil {
		argSpecs, flagSpecs = node.Command.Args, node.Command.Flags
	}
	state := inputs.Scan(argSpecs, flagSpecs, rest)

	switch {
	case state.InRest:
		// The tokens of the rest arg are not ours: let the shell complete files.
		return nil, ShellCompDirectiveDefault
	case state.Pending != nil:
		return valueCompletion([]*inputs.Spec{state.Pending}, toComplete)
	case state.AfterDD:
		return valueCompletion(state.Next, toComplete)
	case strings.HasPrefix(toComplete, "-"):
		if strings.Contains(toComplete, "=") {
			return flagValueMatches(flagSpecs, toComplete)
		}
		return filterMatches(flagSpecMatches(flagSpecs, state.Used), toComplete), ShellCompDirectiveNoFileComp
	case len(rest) == 0:
		matches := childMatches(source, loader, node, n == 0)
		if dryRun && n == 0 {
			matches = slices.DeleteFunc(matches, func(m Match) bool { return IsReservedName(m.Text) })
		}
		matches = filterMatches(matches, toComplete)
		if len(matches) > 0 {
			return matches, ShellCompDirectiveNoFileComp
		}
		// No subcommand fits the prefix: it is the first positional word.
		return valueCompletion(state.Next, toComplete)
	}
	return valueCompletion(state.Next, toComplete)
}

// flagValueMatches completes the `--name=value` form: the choices of the flag
// with the "--name=" prefix, nothing for other flags (their values are free
// text; the shell does not complete a value glued to the flag).
func flagValueMatches(flags []inputs.Spec, toComplete string) ([]Match, Directive) {
	name := flagName(toComplete)
	for _, f := range flags {
		if cliName(f.Name) != name {
			continue
		}
		var matches []Match
		for _, c := range f.CanonicalChoices() {
			matches = append(matches, Match{Text: "--" + name + "=" + c})
		}
		return filterMatches(matches, toComplete), ShellCompDirectiveNoFileComp
	}
	return nil, ShellCompDirectiveNoFileComp
}

// valueCompletion suggests a value for any of specs. The wider class wins: a
// file input or the rest arg (its words belong to the wrapped program) lets the
// shell complete files; else a dir input limits the shell to directories; else
// the union of the choices is suggested; else there is nothing to suggest.
func valueCompletion(specs []*inputs.Spec, toComplete string) ([]Match, Directive) {
	var dirs bool
	var matches []Match
	for _, s := range specs {
		switch {
		case s.Rest || s.Type == "file":
			return nil, ShellCompDirectiveDefault
		case s.Type == "dir":
			dirs = true
		default:
			for _, c := range s.CanonicalChoices() {
				matches = append(matches, Match{Text: c})
			}
		}
	}
	if dirs {
		return nil, ShellCompDirectiveFilterDirs
	}
	return filterMatches(matches, toComplete), ShellCompDirectiveNoFileComp
}

// helpPathMatches suggests the path words after `futi help` / `futi list`.
func helpPathMatches(source discovery.Source, loader modulerunner.ModuleLoader, words []string, toComplete string) ([]Match, Directive) {
	node, n, err := modulerunner.Walk(source, loader, words, modulerunner.WalkOptions{Verified: true})
	if err != nil || n != len(words) || strings.HasPrefix(toComplete, "-") {
		return nil, ShellCompDirectiveNoFileComp
	}
	return filterMatches(childMatches(source, loader, node, false), toComplete), ShellCompDirectiveNoFileComp
}

// childMatches lists the children of node: the subcommands of its command and
// the modules and namespaces below it. At the root a builtin wins over a
// module of the same name; builtins adds them to the list (it is set only
// for the first word of the command line).
func childMatches(source discovery.Source, loader modulerunner.ModuleLoader, node *modulerunner.Node, builtins bool) []Match {
	var matches []Match
	root := len(node.Path) == 0
	if root && builtins {
		for _, b := range rootBuiltins() {
			matches = append(matches, Match{Text: b})
		}
	}
	if node.Command != nil {
		for name, cmd := range node.Command.Commands {
			matches = append(matches, Match{Text: name, Description: cmd.Description})
		}
	}

	// A missing namespace or an unreadable listing gives no children; a
	// partial listing (some sources unreadable) is a valid result.
	children, err := source.ListChildren(node.Namespace)
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return matches
	}
	for _, c := range children {
		if root && builtins && IsReservedName(c.Name) {
			continue
		}
		m := Match{Text: c.Name}
		if c.Type == "module" && !c.Conflict { // a conflicting module is not loaded
			// The module description (zsh); the error is ignored.
			if mod, err := modulerunner.LoadModule(loader, c.ManifestPath, c.ModuleRoot); err == nil {
				m.Description = mod.Description
			}
		}
		matches = append(matches, m)
	}
	return matches
}

// flagSpecMatches lists the flags of a command (--name and -s) and --help.
// A flag already used is skipped unless it is variadic. Each flag carries a
// hint: "(required)" or "(default: X)" — zsh shows it next to the flag.
func flagSpecMatches(flags []inputs.Spec, used map[string]bool) []Match {
	matches := []Match{{Text: "--help", Description: "Show help"}}
	for _, f := range flags {
		if used[f.Name] && !f.Variadic {
			continue
		}
		matches = append(matches, Match{Text: "--" + cliName(f.Name), Description: paramHint(f)})
		if f.Short != "" {
			matches = append(matches, Match{Text: "-" + f.Short, Description: paramHint(f)})
		}
	}
	return matches
}

// builtinMatches suggests for `futi cache|completion|modules|__complete …`.
func builtinMatches(source discovery.Source, args []string, toComplete string) ([]Match, Directive) {
	// After `--` the words are user data: let the shell complete files.
	if slices.Contains(args, "--") {
		return nil, ShellCompDirectiveDefault
	}

	// The words up to the first flag token are the builtin and its subcommand,
	// as in the CLI parser: a flag is a token with "-" longer than one character.
	positionals := args
	var flagArgs []string
	for i, a := range args {
		if len(a) > 1 && a[0] == '-' {
			positionals, flagArgs = args[:i], args[i:]
			break
		}
	}

	// The previous word is a flag without "=" and the current one is not a flag.
	// A bool flag takes no value (the parser: "--force true" is an error), so
	// the remaining flags follow. The value of a string flag is user-supplied:
	// the shell may complete files.
	if len(args) > 0 && strings.HasPrefix(args[len(args)-1], "--") && !strings.Contains(args[len(args)-1], "=") && !strings.HasPrefix(toComplete, "--") {
		if !modulesBoolFlagNext(positionals, flagArgs) {
			return nil, ShellCompDirectiveDefault
		}
		flags := ModulesFlags(positionals[1])
		return filterMatches(withoutUsed(flagMatches(flags, toComplete), flagArgs), toComplete), ShellCompDirectiveNoFileComp
	}

	if strings.HasPrefix(toComplete, "--") {
		// `--flag=val`: the value is user-supplied, files are allowed.
		if strings.Contains(toComplete, "=") {
			return nil, ShellCompDirectiveDefault
		}
		if len(positionals) >= 2 && positionals[0] == "modules" {
			if flags := ModulesFlags(positionals[1]); flags != nil {
				return filterMatches(withoutUsed(flagMatches(flags, toComplete), flagArgs), toComplete), ShellCompDirectiveNoFileComp
			}
		}
		return nil, ShellCompDirectiveNoFileComp
	}

	// After the first flag no path words are possible.
	if len(flagArgs) > 0 {
		return nil, ShellCompDirectiveNoFileComp
	}

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

// modulesBoolFlagNext сообщает, что последний токен flagArgs — булев флаг
// подкоманды `futi modules <sub>`, после которого нужно подсказывать флаги, а
// не файлы. Для install при ещё не введённом <source> — false: источник это
// путь/URL, нужно файловое дополнение.
func modulesBoolFlagNext(positionals, flagArgs []string) bool {
	if len(positionals) < 2 || positionals[0] != "modules" || len(flagArgs) == 0 {
		return false
	}
	sub := positionals[1]
	if ModulesFlags(sub) == nil {
		return false
	}
	valueFlags := make(map[string]bool)
	for _, f := range modulesValueFlags[sub] {
		valueFlags[f] = true
	}
	last := flagArgs[len(flagArgs)-1]
	if !slices.Contains(ModulesFlags(sub), last) || valueFlags[last] {
		return false
	}
	if sub != "install" {
		return true
	}
	// <source> уже введён: позиционный после install или среди флагов
	// (значения флагов со значением не считаются).
	if len(positionals) > 2 {
		return true
	}
	rest := flagArgs[:len(flagArgs)-1]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if len(a) > 1 && a[0] == '-' {
			if valueFlags[a] {
				i++
			}
			continue
		}
		return true
	}
	return false
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

// cliName is the spelling of an input name on the command line.
func cliName(name string) string { return strings.ReplaceAll(name, "_", "-") }

// paramHint формирует компактную пометку параметра для подсказки.
func paramHint(p inputs.Spec) string {
	if p.Required {
		return "(required)"
	}
	if p.HasDefault && p.Default != "" {
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
