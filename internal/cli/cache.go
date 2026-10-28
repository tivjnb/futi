package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
)

// cacheSubcommand извлекает cache subcommand из args (args[1:], переданные в Main).
// Возвращает (subcommand, true) если первый аргумент — "cache".
// Возвращает ("", false) если "cache" не первый аргумент (обычная команда).
// subcommand="" означает "cache" без подкоманды — main выведет понятную ошибку.
func cacheSubcommand(args []string) (subcommand string, present bool) {
	if len(args) < 1 || args[0] != "cache" {
		return "", false
	}
	if len(args) < 2 {
		return "", true
	}
	return args[1], true
}

// runCacheRefresh явно обновляет кэш: обходит globalSource, перечитывает
// изменившиеся модули, сохраняет на диск. Выводит статистику.
// При verbose=true дополнительно выводит списки обновлённых и пропущенных
// модулей (фикс #15).
func runCacheRefresh(w io.Writer, cache *modulecache.Cache, globalSource discovery.Source, verbose bool) error {
	result, err := core.RefreshCache(cache, globalSource)
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return err
	}
	// Частичный листинг: сводка по читаемым sources выводится, ошибка
	// возвращается в конце — вызывающий покажет предупреждения.
	fmt.Fprintf(w, "Cache refreshed: %d modules, %d updated", result.Total, result.Updated)
	if result.Skipped > 0 {
		fmt.Fprintf(w, ", %d skipped", result.Skipped)
	}
	fmt.Fprintln(w)

	if verbose {
		if len(result.UpdatedModules) > 0 {
			fmt.Fprintln(w, "Updated:")
			for _, path := range result.UpdatedModules {
				fmt.Fprintf(w, "  %s\n", path)
			}
		}
		if len(result.SkippedModules) > 0 {
			fmt.Fprintln(w, "Skipped:")
			for _, sm := range result.SkippedModules {
				fmt.Fprintf(w, "  %s (%s)\n", sm.Path, sm.Reason)
			}
		}
	}
	return err
}

// runCacheStatus выводит текущее состояние кэша: политику, время последнего
// обновления, количество модулей, путь к gob-файлу (фикс #17).
func runCacheStatus(w io.Writer, cache *modulecache.Cache, cachePath string) error {
	st := core.CacheStatusOf(cache, cachePath)
	fmt.Fprintf(w, "Policy: %s\n", st.Policy)

	if st.LastRefresh.IsZero() {
		fmt.Fprintln(w, "Last refresh: never")
	} else {
		fmt.Fprintf(w, "Last refresh: %s\n", st.LastRefresh.Format("2006-01-02 15:04:05"))
	}

	fmt.Fprintf(w, "Modules: %d\n", st.Modules)

	if st.Path == "" {
		fmt.Fprintln(w, "Storage: in-memory (no persistent cache)")
	} else {
		fmt.Fprintf(w, "Cache file: %s\n", st.Path)
	}
	return nil
}

// runCacheClear удаляет gob-файл кэша с диска (фикс #17).
// In-memory кэш не очищается — он живёт только в текущем процессе.
func runCacheClear(w io.Writer, cachePath string) error {
	if cachePath == "" {
		fmt.Fprintln(w, "cache: in-memory only, nothing to clear")
		return nil
	}
	if err := core.ClearCache(cachePath); err != nil {
		return err
	}
	fmt.Fprintf(w, "Cache cleared: %s\n", cachePath)
	return nil
}
