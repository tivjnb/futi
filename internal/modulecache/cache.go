// Package modulecache — persistent кэш загруженных модулей.
//
// Архитектура (см. docs/superpowers/specs/2026-08-19-persistent-modulecache-design.md):
//   - Storage interface — абстракция над persistent/in-memory хранилищем.
//   - DiskStorage — gob-файл на диске (используется CLI).
//   - MemoryStorage — no-op fallback (нет home dir) и заготовка для HTTP server.
//   - RefreshPolicy interface — стратегия обновления (always/every_t_min/force_only).
//   - Cache — orchestrator: in-memory entries (ALL модули) + persistent (только глобальные).
//
// In-memory кэш хранит все модули (глобальные + локальные) в рамках процесса —
// решает двойную загрузку (localSource + Run). Persistent кэш на диске хранит
// только глобальные — локальные не загрязняют общий кэш.
package modulecache

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"futi/internal/discovery"
	"futi/internal/execmodule"
)

// Cache — orchestrator кэша модулей.
//
// In-memory entries хранят ВСЕ модули (глобальные из persistent + локальные transient).
// persistentPaths — set путей глобальных модулей (сохраняются на диск при Save).
// Потокобезопасен: параллельные вызовы LoadModule синхронизируются мьютексом.
type Cache struct {
	mu              sync.Mutex
	storage         Storage
	policy          RefreshPolicy
	entries         map[string]entry  // in-memory: ALL (global + local)
	persistentPaths map[string]bool   // set путей глобальных модулей
	lastRefresh     time.Time         // для every_t_min политики и cacheFile
}

// New создаёт Cache с заданными Storage и RefreshPolicy (DI).
// entries и persistentPaths инициализируются пустыми — заполняются при Start/Refresh/LoadModule.
func New(storage Storage, policy RefreshPolicy) *Cache {
	return &Cache{
		storage:         storage,
		policy:          policy,
		entries:         make(map[string]entry),
		persistentPaths: make(map[string]bool),
	}
}

// LoadModule возвращает модуль из in-memory кэша или загружает с диска.
// Реализует интерфейс modulerunner.ModuleLoader.
//
// Cache hit: path в entries → вернуть entries[path].Module (работает для всех —
// и глобальных, и локальных).
// Cache miss: загрузить через execmodule.LoadModule, добавить в entries (in-memory),
// НО НЕ в persistentPaths. Локальные модули не сохраняются на диск.
//
// Ключ кэша — manifestPath. moduleRoot в ключ не входит.
// Ошибки загрузки не кэшируются.
func (c *Cache) LoadModule(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[manifestPath]; ok {
		return e.Module, nil
	}

	mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
	if err != nil {
		return nil, err
	}

	c.entries[manifestPath] = entry{Module: mod}
	return mod, nil
}

// Load загружает entries из storage без вызова Refresh. Возвращает cacheFile
// для introspection (политика, версия). Используется когда Refresh будет вызван
// явно (`futi cache refresh`) — устраняет двойной Refresh (Start + runCacheRefresh).
//
// Ошибка Load (повреждённый/несовместимый файл) — не фатальна: возвращается
// пустая cacheFile, entries остаются пустыми. При последующем Refresh кэш
// заполнится заново.
func (c *Cache) Load() cacheFile {
	cf, err := c.storage.Load()
	if err != nil {
		return cacheFile{}
	}
	c.mu.Lock()
	for path, e := range cf.Entries {
		c.entries[path] = e
		c.persistentPaths[path] = true
	}
	c.lastRefresh = cf.LastRefreshTime
	c.mu.Unlock()
	return cf
}

// Start вызывается один раз при старте CLI:
// 1. Load() → заполняет entries и persistentPaths (глобальные с диска).
// 2. Если policy.ShouldRefresh(state) → Refresh(globalSource).
// 3. saveToStorage — сохраняет только persistentPaths.
//
// Ошибка Load (повреждённый/несовместимый файл) — не фатальна: rebuild с пустого
// состояния. Если политика требует Refresh — кэш заполнится заново.
// Ошибка Save — не фатальна: in-memory кэш актуален.
func (c *Cache) Start(globalSource discovery.Source) error {
	cf := c.Load()

	if c.policy.ShouldRefresh(cf) {
		if _, err := c.Refresh(globalSource); err != nil {
			return fmt.Errorf("modulecache: start: refresh: %w", err)
		}
	} else {
		// Сохраняем даже без Refresh — обновляем Version если был старый формат,
		// или очищаем повреждённый файл.
		if err := c.saveToStorage(); err != nil {
			// Не фатально: in-memory кэш актуален. Логируем warning.
			log.Printf("modulecache: start: save: %v", err)
		}
	}

	return nil
}

// RefreshResult — статистика обновления кэша.
type RefreshResult struct {
	Total   int // всего модулей в source
	Updated int // перечитано (изменилось)
	Skipped int // пропущено из-за ошибок Stat/LoadModule
}

// Refresh обходит source, перечитывает изменившиеся модули и сохраняет на диск.
// Вызывается из Start (по политике) и из `futi cache refresh` (явно).
//
// Для каждого модуля из globalSource.ListChildrenRecursive(""):
// 1. os.Stat(manifestPath) → получить mtime, size.
// 2. Сравнить с entries[path] — если mtime+size совпадают, пропустить.
// 3. Иначе: execmodule.LoadModule → обновить entries[path], добавить в persistentPaths.
// 4. Cleanup: удалить из entries/persistentPaths те, кого больше нет в source.
// 5. Обновить lastRefresh (только если не все модули пропущены).
// 6. saveToStorage.
//
// Ошибки Stat/LoadModule не фатальны: модуль пропускается, счётчик Skipped
// инкрементируется, ошибка логируется. При следующем Refresh retry.
// Ошибка Save не фатальна: in-memory кэш актуален, логируется.
func (c *Cache) Refresh(globalSource discovery.Source) (RefreshResult, error) {
	children, err := globalSource.ListChildrenRecursive("")
	if err != nil {
		return RefreshResult{}, fmt.Errorf("modulecache: refresh: list source: %w", err)
	}

	result := RefreshResult{}

	// seenPaths — множество путей модулей, обнаруженных в source в этом цикле.
	// Используется для cleanup: то, чего нет в source, удаляется из кэша.
	seenPaths := make(map[string]bool, len(children))

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, child := range children {
		if child.Type != "module" {
			continue
		}
		result.Total++
		seenPaths[child.ManifestPath] = true

		info, err := os.Stat(child.ManifestPath)
		if err != nil {
			// Не можем stat-нуть — пропускаем (модуль мог удалиться между list и stat).
			// Не фатально: при следующем Refresh будет retry. Модуль остаётся в кэше,
			// т.к. он есть в source (seenPaths) — временная ошибка не должна чистить кэш.
			result.Skipped++
			log.Printf("modulecache: refresh: stat %q: %v", child.ManifestPath, err)
			continue
		}

		// Проверяем: изменился ли файл (mtime + size).
		if e, ok := c.entries[child.ManifestPath]; ok {
			if e.ModTime.Equal(info.ModTime()) && e.Size == info.Size() {
				// Не изменился — пропускаем, но убеждаемся что path в persistentPaths.
				c.persistentPaths[child.ManifestPath] = true
				continue
			}
		}

		// Файл изменился (или новый) — перечитываем.
		mod, err := execmodule.LoadModule(child.ManifestPath, child.ModuleRoot)
		if err != nil {
			// Не фатально: пропускаем модуль с ошибкой. При следующем Refresh retry.
			result.Skipped++
			log.Printf("modulecache: refresh: load %q: %v", child.ManifestPath, err)
			continue
		}

		c.entries[child.ManifestPath] = entry{
			Module:  mod,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}
		c.persistentPaths[child.ManifestPath] = true
		result.Updated++
	}

	// Cleanup: удаляем из кэша модули, которых больше нет в source.
	// Трогаем только persistentPaths (глобальные модули) — локальные (in-memory only)
	// добавляются через LoadModule в течение сессии и не должны чиститься Refresh.
	for path := range c.persistentPaths {
		if !seenPaths[path] {
			delete(c.entries, path)
			delete(c.persistentPaths, path)
		}
	}

	// Обновляем lastRefresh, только если не все модули пропущены.
	// Если все упали (Skipped == Total > 0) — кэш пустой/неполный, не обновляем
	// lastRefresh, чтобы every_t_min политика дала immediate retry.
	// Пустой source (Total == 0) — валидное состояние, обновляем.
	if result.Total == 0 || result.Skipped < result.Total {
		c.lastRefresh = time.Now()
	}

	// Save — ошибка не фатальна (in-memory актуален). Логируем warning.
	if err := c.saveToStorage(); err != nil {
		log.Printf("modulecache: refresh: save: %v", err)
	}

	return result, nil
}

// saveToStorage сохраняет только persistent entries на диск.
// Фильтрует entries по persistentPaths, формирует cacheFile, вызывает storage.Save.
func (c *Cache) saveToStorage() error {
	persistentEntries := make(map[string]entry, len(c.persistentPaths))
	for path := range c.persistentPaths {
		if e, ok := c.entries[path]; ok {
			persistentEntries[path] = e
		}
	}

	cf := cacheFile{
		Version:         cacheFileVersion,
		LastRefreshTime: c.lastRefresh,
		Entries:         persistentEntries,
	}

	return c.storage.Save(cf)
}
