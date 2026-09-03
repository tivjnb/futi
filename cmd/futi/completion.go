package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"futi/internal/completion"
	"futi/internal/config"
	"futi/internal/discovery"
	"futi/internal/modulecache"
	"futi/internal/modulerunner"
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
		if m.Description != "" {
			fmt.Fprintf(w, "%s\t%s\n", m.Text, m.Description)
		} else {
			fmt.Fprintln(w, m.Text)
		}
	}
	fmt.Fprintf(w, ":%d\n", int(directive))
	if stderr != nil {
		fmt.Fprintf(stderr, "Completion ended with directive: %s\n", directive)
	}
	return nil
}

// runCompleteMain — оркестрация `futi __complete`: строит source (local + global),
// кэш, прогревает его при необходимости и вызывает runComplete.
// Возвращает код завершения: 0 — подсказки выведены, 1 — ошибка.
func runCompleteMain(stdout, stderr io.Writer, cfg *config.Config, globalSource discovery.Source, args []string) int {
	cache := newCacheFromConfig(cfg)
	cache.Load()

	// Самообновление: если кэш холодный/устаревший — прогреваем один раз.
	// Ошибка Refresh не фатальна: completion продолжит с тем, что есть.
	if completionNeedsRefresh(cache) {
		if _, err := cache.Refresh(globalSource); err != nil {
			fmt.Fprintf(stderr, "__complete: cache refresh: %v (continuing with existing data)\n", err)
		}
	}

	source, err := createSource(cache, globalSource)
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
// Возвращает true если кэш пуст (не прогревался), никогда не обновлялся или
// обновлялся слишком давно (см. completionCacheMaxAge).
func completionNeedsRefresh(cache *modulecache.Cache) bool {
	if cache.ModuleCount() == 0 {
		return true
	}
	lr := cache.LastRefresh()
	if lr.IsZero() {
		return true
	}
	return time.Since(lr) > completionCacheMaxAge
}
