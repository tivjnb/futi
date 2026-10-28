package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tivjnb/futi/internal/completion"
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
			argsList:   argsList,
		}
	}

	pa := &parsedArgs{
		flags:    flags,
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

// commandPath восстанавливает позиционный путь (сегменты namespace + команда)
// разобранной команды — тот же, что Run получает как positional.
func commandPath(pa *parsedArgs) []string {
	path := strings.Split(pa.namespace, ".")
	if pa.command != "" {
		path = append(path, pa.command)
	}
	return path
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
