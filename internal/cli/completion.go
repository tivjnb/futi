package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tivjnb/futi/internal/completion"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
	"github.com/tivjnb/futi/internal/modulerunner"
	"github.com/tivjnb/futi/internal/termtext"
)

const (
	completionBash = "bash"
	completionZsh  = "zsh"
	completionFish = "fish"
)

// completionCacheMaxAge — насколько устаревший кэш ещё допустим для completion
// без самообновления. Если кэш пуст (не создавался), никогда не обновлялся или
// обновлялся давнее этого порога — `__complete` прогревает его через Refresh.
// Это компромисс: обычный CLI с политикой every_time и так обновляет кэш при
// каждом запуске, поэтому здесь достаточно редкого самообновления (латентность
// completion на каждый TAB не растёт, а холодный/сильно устаревший кэш лечится).
const completionCacheMaxAge = time.Hour

// validCompletionShells — поддерживаемые shell (для сообщения об ошибке).
var validCompletionShells = []string{completionBash, completionZsh, completionFish}

// runCompletion выводит скрипт completion для заданного shell.
// Возвращает ошибку при неизвестном shell с перечислением валидных.
func runCompletion(w io.Writer, shell string) error {
	var script string
	switch shell {
	case completionBash:
		script = bashCompletionScript
	case completionZsh:
		script = zshCompletionScript
	case completionFish:
		script = fishCompletionScript
	default:
		return fmt.Errorf("unknown shell %q (valid: %s)", shell, strings.Join(validCompletionShells, ", "))
	}
	fmt.Fprint(w, script)
	return nil
}

// sanitizeCompletionText подготавливает текст/описание кандидата к построчному
// выводу в __complete. Управляющие символы (\t, \n, \r) заменяются на пробел,
// прочие (ESC и др.) удаляются (termtext.Line):
// таб искажает формат "text\tdescription", перевод строки разлагает кандидата
// на несколько строк и ломает протокол (bash/zsh/fish парсят вывод построчно).
// REV-010: значения приходят из имён файлов и YAML-полей — не доверяем им.
func sanitizeCompletionText(s string) string {
	return termtext.Line(s)
}

// runComplete — hidden-обработчик `futi __complete <args...>`.
// Возвращает подсказки построчно в stdout; последняя строка — ":N" (directive).
// Вход args — все аргументы после `futi __complete`; последний = toComplete
// (текущее слово под курсором, может быть ""), всё до него = args.
//
// Формат совместим с cobra: при наличии описания подсказка выводится как
// "text\tdescription" (zsh парсит таб, bash обрезает до первого таба).
func runComplete(w io.Writer, stderr io.Writer, source discovery.Source, loader modulerunner.ModuleLoader, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("__complete: missing toComplete argument")
	}
	toComplete := args[len(args)-1]
	completeArgs := args[:len(args)-1]

	matches, directive := completion.Complete(source, loader, completeArgs, toComplete)
	for _, m := range matches {
		text := sanitizeCompletionText(m.Text)
		desc := sanitizeCompletionText(m.Description)
		if desc != "" {
			fmt.Fprintf(w, "%s\t%s\n", text, desc)
		} else {
			fmt.Fprintln(w, text)
		}
	}
	fmt.Fprintf(w, ":%d\n", int(directive))
	if stderr != nil {
		fmt.Fprintf(stderr, "Completion ended with directive: %s\n", directive)
	}
	return nil
}

// runCompleteMain — оркестрация `futi __complete`: берёт у Runtime source (local + global),
// кэш, прогревает его при необходимости и вызывает runComplete.
// Возвращает код завершения: 0 — подсказки выведены, 1 — ошибка.
func runCompleteMain(stdout, stderr io.Writer, rt *core.Runtime, args []string) int {
	cache := rt.Cache()
	cache.Load()

	// Самообновление: если кэш холодный/устаревший — прогреваем один раз.
	// Ошибка Refresh не фатальна: completion продолжит с тем, что есть.
	if completionNeedsRefresh(cache) {
		// Частичный листинг (нечитаемый source) в completion молчит.
		var partial *discovery.PartialError
		if _, err := cache.Refresh(rt.GlobalSource()); err != nil && !errors.As(err, &partial) {
			fmt.Fprintf(stderr, "__complete: cache refresh: %v (continuing with existing data)\n", err)
		}
	}

	source, err := rt.Source()
	if err != nil {
		fmt.Fprintf(stderr, "__complete: %v\n", err)
		return 1
	}

	if err := runComplete(stdout, stderr, source, cache, args); err != nil {
		fmt.Fprintf(stderr, "__complete: %v\n", err)
		return 1
	}
	return 0
}

// completionNeedsRefresh решает, стоит ли `__complete` обновить кэш перед работой.
// Возвращает true если кэш никогда не обновлялся или обновлялся слишком давно
// (см. completionCacheMaxAge). Число модулей не учитывается: пустой каталог
// глобальных модулей после Refresh — валидный свежий кэш, иначе каждый TAB
// переписывал бы файл кэша. Если все модули пропущены (Skipped == Total > 0),
// Refresh не обновляет lastRefresh, и обновление на каждый TAB остаётся.
func completionNeedsRefresh(cache *modulecache.Cache) bool {
	lr := cache.LastRefresh()
	if lr.IsZero() {
		return true
	}
	return time.Since(lr) > completionCacheMaxAge
}
