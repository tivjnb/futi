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
	// words — the command path and its inputs as given; the path ends where
	// core.Runtime.ResolveWords stops.
	words  []string
	dryRun bool // глобальный --dry-run: напечатать команду, не исполняя
}

// listRequest сигнализирует, что запрошен листинг namespace или модуля.
type listRequest struct {
	namespace string
	verbose   bool
	recurse   bool
}

func (e *listRequest) Error() string { return "list command requested" }
func (e *listRequest) Unwrap() error { return errListCommand }

// helpRequest is `futi help <path...>`: system help for the node at path.
type helpRequest struct{ path []string }

func (e *helpRequest) Error() string { return "help requested" }

// parseArgs parses CLI arguments. The first words list and help are builtin
// commands (`futi list [path...] [-v] [-r]`, `futi help <path...>`).
// Otherwise args are a command path and its inputs: the walk over the module
// tree (core.Runtime.ResolveWords) finds where the path ends; tokens after it
// up to `--` are raw flags (bound by bindFlags), everything after `--` is the
// command args. A flag or `--` as the first token is a usage error (errHelp if
// the flags contain -h/--help).
func parseArgs(args []string) (*parsedArgs, error) {
	if len(args) > 0 {
		switch args[0] {
		case "list":
			path, flags := splitPathFlags(args[1:])
			return nil, parseListRequest(path, flags)
		case "help":
			return nil, &helpRequest{path: args[1:]}
		}
	}
	if len(args) == 0 || isFlagToken(args[0]) || args[0] == "--" {
		_, flags := splitPathFlags(args)
		if hasHelpFlag(flags) {
			return nil, errHelp
		}
		usage := "usage: futi <namespace...> <command> [--param value] [-- args]"
		if len(flags) > 0 {
			usage += " (" + pathBeforeFlagsHint + ")"
		}
		return nil, errors.New(usage)
	}
	return &parsedArgs{words: args}, nil
}

// splitPathFlags splits args into the leading words (up to the first flag
// token or `--`) and the flags after them up to `--`; tokens after `--` are
// dropped.
func splitPathFlags(args []string) (path, flags []string) {
	i := 0
	for ; i < len(args) && !isFlagToken(args[i]) && args[i] != "--"; i++ {
		path = append(path, args[i])
	}
	flags, _ = splitInputs(args[i:])
	return path, flags
}

// splitInputs splits the words after a command path into raw flags (before
// the first `--`) and the command args (after it).
func splitInputs(rest []string) (flags, argsList []string) {
	for i, tok := range rest {
		if tok == "--" {
			return flags, append([]string(nil), rest[i+1:]...)
		}
		flags = append(flags, tok)
	}
	return flags, nil
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
