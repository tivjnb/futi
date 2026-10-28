package cli

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
)

// TestRunCacheRefreshVerboseShowsUpdatedModules проверяет, что при verbose=true
// runCacheRefresh выводит пути обновлённых модулей.
func TestRunCacheRefreshVerboseShowsUpdatedModules(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeMainTestFile(t, modPath, "name: app\ncmd: echo\n")

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	var buf bytes.Buffer
	if err := runCacheRefresh(&buf, cache, src, true); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Updated:") {
		t.Fatalf("expected 'Updated:' section in verbose output, got: %q", output)
	}
	if !strings.Contains(output, modPath) {
		t.Fatalf("expected module path %q in verbose output, got: %q", modPath, output)
	}
}

// TestRunCacheRefreshVerboseShowsSkippedModules проверяет, что при verbose=true
// runCacheRefresh выводит пропущенные модули с причиной.
func TestRunCacheRefreshVerboseShowsSkippedModules(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "good.yaml"), "name: good\ncmd: echo\n")
	writeMainTestFile(t, filepath.Join(dir, "bad.yaml"), "name: bad\ncommands:\n  empty: {}\n")

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	oldOut := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldOut)

	var buf bytes.Buffer
	if err := runCacheRefresh(&buf, cache, src, true); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Skipped:") {
		t.Fatalf("expected 'Skipped:' section in verbose output, got: %q", output)
	}
	badPath := filepath.Join(dir, "bad.yaml")
	if !strings.Contains(output, badPath) {
		t.Fatalf("expected skipped module path %q in verbose output, got: %q", badPath, output)
	}
}

// TestRunCacheRefreshNonVerboseHidesDetails проверяет, что при verbose=false
// runCacheRefresh НЕ выводит детали (только сводку).
func TestRunCacheRefreshNonVerboseHidesDetails(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeMainTestFile(t, modPath, "name: app\ncmd: echo\n")

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	var buf bytes.Buffer
	if err := runCacheRefresh(&buf, cache, src, false); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "Updated:") {
		t.Fatalf("verbose details should not appear in non-verbose output, got: %q", output)
	}
	if strings.Contains(output, modPath) {
		t.Fatalf("module path should not appear in non-verbose output, got: %q", output)
	}
}

// --- #17: cache status and cache clear ---

// TestRunCacheStatusOutput проверяет, что runCacheStatus выводит политику,
// last refresh, количество модулей и путь к gob-файлу.
func TestRunCacheStatusOutput(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "app.yaml"), "name: app\ncmd: echo\n")

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	if _, err := cache.Refresh(src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	cachePath := filepath.Join(t.TempDir(), "modulecache.gob")
	var buf bytes.Buffer
	if err := runCacheStatus(&buf, cache, cachePath); err != nil {
		t.Fatalf("runCacheStatus: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "every_time") {
		t.Fatalf("expected policy name 'every_time' in output, got: %q", output)
	}
	if !strings.Contains(output, "Modules: 1") {
		t.Fatalf("expected 'Modules: 1' in output, got: %q", output)
	}
	if !strings.Contains(output, cachePath) {
		t.Fatalf("expected cache path %q in output, got: %q", cachePath, output)
	}
}

// TestRunCacheStatusInMemory проверяет, что runCacheStatus показывает
// in-memory при пустом пути.
func TestRunCacheStatusInMemory(t *testing.T) {
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	var buf bytes.Buffer
	if err := runCacheStatus(&buf, cache, ""); err != nil {
		t.Fatalf("runCacheStatus: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "in-memory") {
		t.Fatalf("expected 'in-memory' in output, got: %q", output)
	}
}

// TestRunCacheClearRemovesGobFile проверяет, что runCacheClear удаляет
// gob-файл кэша.
func TestRunCacheClearRemovesGobFile(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "modulecache.gob")
	if err := os.WriteFile(cachePath, []byte("cache data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	if err := runCacheClear(&buf, cachePath); err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}

	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("expected gob file removed, got err: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "cleared") {
		t.Fatalf("expected 'cleared' in output, got: %q", output)
	}
}

// TestRunCacheClearMissingFile проверяет, что runCacheClear не падает,
// если gob-файл не существует.
func TestRunCacheClearMissingFile(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "nonexistent.gob")
	var buf bytes.Buffer
	if err := runCacheClear(&buf, cachePath); err != nil {
		t.Fatalf("runCacheClear should not fail on missing file: %v", err)
	}
}

// --- Group 3a: CLI cache command (#2, #3, #4, #25) ---

// TestCacheSubcommandRefresh проверяет, что "cache refresh" определяется
// как subcommand="refresh".
func TestCacheSubcommandRefresh(t *testing.T) {
	subcmd, ok := cacheSubcommand([]string{"cache", "refresh"})
	if !ok {
		t.Fatal("expected ok=true for cache refresh")
	}
	if subcmd != "refresh" {
		t.Fatalf("expected subcmd=refresh, got %q", subcmd)
	}
}

// TestCacheSubcommandWithoutSubcommand проверяет, что "cache" без subcommand
// возвращает ("", true) — main выведет понятную ошибку "requires a subcommand".
func TestCacheSubcommandWithoutSubcommand(t *testing.T) {
	subcmd, ok := cacheSubcommand([]string{"cache"})
	if !ok {
		t.Fatal("expected ok=true for cache without subcommand")
	}
	if subcmd != "" {
		t.Fatalf("expected empty subcmd, got %q", subcmd)
	}
}

// TestCacheSubcommandUnknown проверяет, что "cache foo" возвращает ("foo", true).
func TestCacheSubcommandUnknown(t *testing.T) {
	subcmd, ok := cacheSubcommand([]string{"cache", "foo"})
	if !ok {
		t.Fatal("expected ok=true for cache with unknown subcommand")
	}
	if subcmd != "foo" {
		t.Fatalf("expected subcmd=foo, got %q", subcmd)
	}
}

// TestCacheSubcommandNotCache проверяет, что обычные команды не определяются
// как cache subcommand.
func TestCacheSubcommandNotCache(t *testing.T) {
	_, ok := cacheSubcommand([]string{"docker", "build"})
	if ok {
		t.Fatal("expected ok=false for non-cache command")
	}
}

// TestCacheSubcommandEmpty проверяет пустой args.
func TestCacheSubcommandEmpty(t *testing.T) {
	_, ok := cacheSubcommand([]string{})
	if ok {
		t.Fatal("expected ok=false for empty args")
	}
}

// TestRunCacheRefreshOutputFormat проверяет формат вывода runCacheRefresh:
// "Cache refreshed: N modules, M updated" + опционально ", K skipped".
// Спека, раздел 8: "выводит статистику; exit 0".
func TestRunCacheRefreshOutputFormat(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "app.yaml"), "name: app\ncmd: echo\n")

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	var buf bytes.Buffer
	if err := runCacheRefresh(&buf, cache, src, false); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Cache refreshed:") {
		t.Fatalf("expected 'Cache refreshed:' in output, got: %q", output)
	}
	if !strings.Contains(output, "1 modules") {
		t.Fatalf("expected '1 modules' in output, got: %q", output)
	}
	if !strings.Contains(output, "1 updated") {
		t.Fatalf("expected '1 updated' in output, got: %q", output)
	}
}

// TestRunCacheRefreshShowsSkipped проверяет, что skipped модули показываются
// в выводе runCacheRefresh (часть #7 — observable counter).
func TestRunCacheRefreshShowsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "good.yaml"), "name: good\ncmd: echo\n")
	writeMainTestFile(t, filepath.Join(dir, "bad.yaml"), "name: bad\ncommands:\n  empty: {}\n")

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	// Подавляем лог от skipped-модуля.
	oldOut := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldOut)

	var buf bytes.Buffer
	if err := runCacheRefresh(&buf, cache, src, false); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "1 skipped") {
		t.Fatalf("expected '1 skipped' in output, got: %q", output)
	}
}

// --- Group 3b: config loading + tests (#5, #12, #26, #27) ---

func TestRunCacheRefreshPartialPrintsSummary(t *testing.T) {
	src, _, _ := partialSource(t)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	var buf bytes.Buffer
	err := runCacheRefresh(&buf, cache, src, false)
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("runCacheRefresh err = %v, want *discovery.PartialError", err)
	}
	if !strings.Contains(buf.String(), "Cache refreshed: 1 modules") {
		t.Errorf("нет сводки:\n%s", buf.String())
	}
}
