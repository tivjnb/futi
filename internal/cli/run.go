package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/inputs"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// raiseInterrupt обрабатывает ошибку запуска команды. Если
// ребёнок убит прерывающим сигналом, futi поднимает тот же сигнал на себя и
// гибнет от него: шелл (bash) иначе решит, что команда сама обработала
// Ctrl+C, и не прервёт цикл. Код выхода шелла остаётся 128+N — его вызывающий
// берёт из core.ExitCode(err).
func raiseInterrupt(err error) {
	if sig, ok := executor.InterruptSignal(err); ok {
		signal.Reset(sig)
		// На Windows Signal вернёт ошибку: тогда сразу выходим с кодом.
		if p, findErr := os.FindProcess(os.Getpid()); findErr == nil {
			if p.Signal(sig) == nil {
				// Доставка сигнала асинхронна; обычно процесс умирает здесь.
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
}

// formatRunError форматирует ошибку выполнения команды для вывода в stderr.
func formatRunError(err error) string {
	return fmt.Sprintf("run: %v", err)
}

// runErrorText — текст ошибки запуска: короткий, если причина уже напечатана
// предупреждением, иначе formatRunError.
func runErrorText(err error, path []string) string {
	if coveredByWarning(err) {
		return coveredReadMessage("run", strings.Join(path, "."))
	}
	return formatRunError(err)
}

// resolveErrorText is the text of a path resolution error: path grammar
// errors (unknown word, missing subcommand, collision) are printed as is,
// others via runErrorText.
func resolveErrorText(err error, path []string) string {
	var pathErr *modulerunner.PathError
	var collision *modulerunner.CollisionError
	if errors.As(err, &pathErr) || errors.As(err, &collision) {
		return err.Error()
	}
	return runErrorText(err, path)
}

// runParsed — общий путь исполнения команды (rt.ResolveWords → inputs.Parse
// (-h/--help → справка) → rt.Execute → formatRunError). Пишет в переданные
// stdout/stderr и возвращает код выхода. Второе значение — ошибка запуска
// (уже напечатанная в stderr): её возвращаем наружу, чтобы вызывающий через
// raiseInterrupt корректно обработал сигнальный путь (процесс гибнет тем же
// сигналом), а Main возвращает код, и тесты проверяют его без os.Exit.
func runParsed(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, rt *core.Runtime, pa *parsedArgs) (int, error) {
	source, _ := rt.Source() // main has already checked the error
	t, rest, err := rt.ResolveWords(pa.words)
	if err != nil {
		// -h/--help after a path without a runnable command (futi docker -h):
		// help for the leading words.
		path, flags := splitPathFlags(pa.words)
		if hasHelpFlag(flags) {
			return systemHelpCode(stdout, stderr, source, rt.Cache(), path), nil
		}
		fmt.Fprintf(stderr, "%s\n", withValidateHint(resolveErrorText(err, path), err, path, source))
		return core.ExitCode(err), err
	}
	path := pa.words[:len(pa.words)-len(rest)]
	return runTarget(ctx, stdin, stdout, stderr, rt, t, path, rest, pa.dryRun)
}

// withSubcommandHint appends the path hint to a parse error when a word
// naming a subcommand of cmd follows a chain of flags that take no value:
// the user put flags before the path (futi db --verbose migrate). A word that
// is the value of a flag, or that follows a positional, is not a path word.
func withSubcommandHint(msg string, cmd *execmodule.Command, rest []string) string {
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if tok == "--" {
			break
		}
		if !modulerunner.IsFlagToken(tok) {
			if _, ok := cmd.Commands[tok]; ok && i > 0 {
				return msg + " (" + modulerunner.PathBeforeFlagsHint + ")"
			}
			break
		}
		if flagTakesValue(cmd, tok) {
			i++ // the next token is the value
		}
	}
	return msg
}

// flagTakesValue reports that tok is a declared non-bool flag written without
// "=": its value is the next token.
func flagTakesValue(cmd *execmodule.Command, tok string) bool {
	for _, f := range cmd.Flags {
		if f.Type == "bool" {
			continue
		}
		if tok == "--"+strings.ReplaceAll(f.Name, "_", "-") || (f.Short != "" && tok == "-"+f.Short) {
			return true
		}
	}
	return false
}

// runTarget runs an already resolved target reached by path: parsing of the
// inputs (-h shows the help), execution, dry-run output and the wait error.
func runTarget(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, rt *core.Runtime, t *core.Target, path, rest []string, dryRun bool) (int, error) {
	source, _ := rt.Source() // main has already checked the error
	cmd := t.Command()
	res, err := inputs.Parse(cmd.Args, cmd.Flags, rest, "futi "+strings.Join(path, " "))
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", withSubcommandHint(err.Error(), cmd, rest))
		return 1, nil
	}
	if res.Help {
		return systemHelpCode(stdout, stderr, source, rt.Cache(), path), nil
	}
	exec, err := rt.Execute(ctx, t, core.ExecuteRequest{
		Inputs: res.Values,
		Stdin:  stdin, Stdout: stdout, Stderr: stderr,
		DryRun: dryRun,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", formatRunError(err))
		return core.ExitCode(err), err
	}
	if dryRun {
		fmt.Fprintln(stdout, shellJoin(exec.Argv))
		return 0, nil
	}
	if exec.WaitErr != nil {
		fmt.Fprintf(stderr, "%s\n", formatRunError(exec.WaitErr))
		return exec.ExitCode, exec.WaitErr
	}
	return 0, nil
}
