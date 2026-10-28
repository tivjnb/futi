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

// runParsed — общий путь исполнения команды (rt.Resolve → bindFlags
// (-h/--help → справка) → rt.Execute → formatRunError) для обычной ветки main и
// пользовательской help. Пишет в переданные stdout/stderr и возвращает код
// выхода. Второе значение — ошибка запуска (уже напечатанная в stderr): её
// возвращаем наружу, чтобы вызывающий через raiseInterrupt корректно
// обработал сигнальный путь (процесс гибнет тем же сигналом), а Main
// возвращает код, и тесты проверяют его без os.Exit.
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
	return runTarget(ctx, stdin, stdout, stderr, rt, t, pa)
}

// runTarget runs an already resolved target: flag binding, -h, execution,
// dry-run output and the wait error.
func runTarget(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, rt *core.Runtime, t *core.Target, pa *parsedArgs) (int, error) {
	source, _ := rt.Source() // main has already checked the error
	loader := rt.Cache()
	helpPath := commandPath(pa)
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
