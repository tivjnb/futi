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
	"bytes"
	"crypto/sha256"
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
// Потокобезопасен: LoadModule использует RLock (параллельные чтения),
// Refresh использует Lock (exclusive). saveToStorage вызывает I/O вне лока.
type Cache struct {
	mu              sync.RWMutex
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
// и глобальных, и локальных). Использует RLock — параллельные чтения разрешены.
// Cache miss: загрузить через execmodule.LoadModule, добавить в entries (in-memory),
// НО НЕ в persistentPaths. Локальные модули не сохраняются на диск.
// Использует double-check pattern: RLock → check → RUnlock; при miss — Lock → re-check → load.
//
// Ключ кэша — manifestPath. moduleRoot в ключ не входит.
// Ошибки загрузки не кэшируются.
func (c *Cache) LoadModule(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	c.mu.RLock()
	if e, ok := c.entries[manifestPath]; ok {
		c.mu.RUnlock()
		return e.Module, nil
	}
	c.mu.RUnlock()

	mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check: другая горутина могла загрузить тот же модуль.
	if e, ok := c.entries[manifestPath]; ok {
		return e.Module, nil
	}
	c.entries[manifestPath] = entry{Module: mod}
	return mod, nil
}

// LoadModuleVerified возвращает модуль, гарантируя его свежесть и аутентичность
// относительно файла манифеста на диске. Используется при ИСПОЛНЕНИИ команды
// (run), где отравленный/устаревший кэш мог бы привести к выполнению чужого кода.
//
// Механизм «чексумма как замок»: на cache-hit читается содержимое файла,
// считается SHA-256 и сравнивается с сохранённым в entry.Hash.
//   - Hash совпал → модуль не менялся, возвращается из кэша (экономит YAML-парсинг).
//   - Hash не совпал → файл изменился или подменён → перечитывается с диска,
//     entry обновляется с новым Hash.
//   - Hash пуст (старый формат кэша, запись добавлена через LoadModule) → запись
//     недоверенна, модуль перечитывается с диска.
//
// В отличие от LoadModule (быстрый, для list/completion/help), эта проверка
// читает файл при каждом cache-hit — применима только на горячем пути исполнения.
func (c *Cache) LoadModuleVerified(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	h, err := hashFile(manifestPath)
	if err != nil {
		return nil, err
	}

	c.mu.RLock()
	if e, ok := c.entries[manifestPath]; ok && len(e.Hash) > 0 && bytes.Equal(e.Hash, h) {
		c.mu.RUnlock()
		return e.Module, nil
	}
	c.mu.RUnlock()

	mod, err := execmodule.LoadModule(manifestPath, moduleRoot)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check: другая горутина могла обновить entry.
	if e, ok := c.entries[manifestPath]; ok && len(e.Hash) > 0 && bytes.Equal(e.Hash, h) {
		return e.Module, nil
	}
	c.entries[manifestPath] = entry{Module: mod, Hash: h}
	return mod, nil
}

// hashFile возвращает SHA-256 содержимого файла.
func hashFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return sum[:], nil
}

// Load загружает entries из storage без вызова Refresh. Возвращает cacheFile
// для introspection (политика, версия). Используется когда Refresh будет вызван
// явно (`futi cache refresh`) — устраняет двойной Refresh (Start + runCacheRefresh).
//
// Ошибка Load (повреждённый/несовместимый файл) — не фатальна: возвращается
// пустая cacheFile, entries остаются пустыми. Ошибка логируется через log.Printf
// (консистентно с Save, который тоже логируется). При последующем Refresh кэш
// заполнится заново.
func (c *Cache) Load() cacheFile {
	cf, err := c.storage.Load()
	if err != nil {
		log.Printf("modulecache: load: %v", err)
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
		// или очищаем повреждённый файл. #14: Save вне лока.
		c.mu.Lock()
		snapshot := c.buildCacheFile()
		c.mu.Unlock()
		if err := c.storage.Save(snapshot); err != nil {
			log.Printf("modulecache: start: save: %v", err)
		}
	}

	return nil
}

// SkippedModule — информация о пропущенном модуле (для --verbose).
type SkippedModule struct {
	Path   string // путь к манифесту
	Reason string // причина пропуска (ошибка Stat/LoadModule)
}

// RefreshResult — статистика обновления кэша.
type RefreshResult struct {
	Total          int             // всего модулей в source
	Updated        int             // перечитано (изменилось)
	Skipped        int             // пропущено из-за ошибок Stat/LoadModule
	UpdatedModules []string        // пути обновлённых модулей (для --verbose)
	SkippedModules []SkippedModule // пропущенные модули с причиной (для --verbose)
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

	for _, child := range children {
		if child.Type != "module" {
			continue
		}
		result.Total++
		seenPaths[child.ManifestPath] = true

		info, err := os.Stat(child.ManifestPath)
		if err != nil {
			result.Skipped++
			result.SkippedModules = append(result.SkippedModules, SkippedModule{
				Path:   child.ManifestPath,
				Reason: fmt.Sprintf("stat: %v", err),
			})
			log.Printf("modulecache: refresh: stat %q: %v", child.ManifestPath, err)
			continue
		}

		// Проверяем: изменился ли файл (mtime + size).
		if e, ok := c.entries[child.ManifestPath]; ok {
			if e.ModTime.Equal(info.ModTime()) && e.Size == info.Size() {
				c.persistentPaths[child.ManifestPath] = true
				continue
			}
		}

		// Файл изменился (или новый) — перечитываем. Считаем SHA-256 содержимого
		// для строгой проверки при исполнении (LoadModuleVerified).
		h, err := hashFile(child.ManifestPath)
		if err != nil {
			result.Skipped++
			result.SkippedModules = append(result.SkippedModules, SkippedModule{
				Path:   child.ManifestPath,
				Reason: fmt.Sprintf("hash: %v", err),
			})
			log.Printf("modulecache: refresh: hash %q: %v", child.ManifestPath, err)
			continue
		}

		mod, err := execmodule.LoadModule(child.ManifestPath, child.ModuleRoot)
		if err != nil {
			result.Skipped++
			result.SkippedModules = append(result.SkippedModules, SkippedModule{
				Path:   child.ManifestPath,
				Reason: fmt.Sprintf("load: %v", err),
			})
			log.Printf("modulecache: refresh: load %q: %v", child.ManifestPath, err)
			continue
		}

		c.entries[child.ManifestPath] = entry{
			Module:  mod,
			ModTime: info.ModTime(),
			Size:    info.Size(),
			Hash:    h,
		}
		c.persistentPaths[child.ManifestPath] = true
		result.Updated++
		result.UpdatedModules = append(result.UpdatedModules, child.ManifestPath)
	}

	// Cleanup: удаляем из кэша модули, которых больше нет в source.
	for path := range c.persistentPaths {
		if !seenPaths[path] {
			delete(c.entries, path)
			delete(c.persistentPaths, path)
		}
	}

	// Обновляем lastRefresh, только если не все модули пропущены.
	if result.Total == 0 || result.Skipped < result.Total {
		c.lastRefresh = time.Now()
	}

	// #14: строим snapshot для Save под локом, затем вызываем Save вне лока.
	cf := c.buildCacheFile()

	c.mu.Unlock()

	// Save вне лока — I/O не блокирует LoadModule.
	if err := c.storage.Save(cf); err != nil {
		log.Printf("modulecache: refresh: save: %v", err)
	}

	return result, nil
}

// buildCacheFile строит snapshot cacheFile из persistent entries.
// Должна вызываться под локом (читает c.entries, c.persistentPaths, c.lastRefresh).
// Вызывающий вызывает storage.Save вне лока (#14: I/O не блокирует LoadModule).
func (c *Cache) buildCacheFile() cacheFile {
	persistentEntries := make(map[string]entry, len(c.persistentPaths))
	for path := range c.persistentPaths {
		if e, ok := c.entries[path]; ok {
			persistentEntries[path] = e
		}
	}
	return cacheFile{
		Version:         cacheFileVersion,
		LastRefreshTime: c.lastRefresh,
		Entries:         persistentEntries,
	}
}

// LastRefresh возвращает время последнего обновления кэша (для futi cache status).
func (c *Cache) LastRefresh() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastRefresh
}

// Invalidate сбрасывает кэш целиком: in-memory entries, persistentPaths,
// lastRefresh и persistent-хранилище (Storage.Clear).
// Вызывается после мутаций файловой системы из `futi modules install/
// uninstall/update`: записи кэша по удалённым/заменённым путям становятся
// stale, а Refresh по политике может не произойти в этом процессе.
func (c *Cache) Invalidate() error {
	c.mu.Lock()
	c.entries = make(map[string]entry)
	c.persistentPaths = make(map[string]bool)
	c.lastRefresh = time.Time{}
	c.mu.Unlock()

	if err := c.storage.Clear(); err != nil {
		return fmt.Errorf("modulecache: invalidate: %w", err)
	}
	return nil
}

// ModuleCount возвращает количество модулей в in-memory кэше (для futi cache status).
func (c *Cache) ModuleCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// PolicyName возвращает имя политики обновления (для futi cache status).
func (c *Cache) PolicyName() string {
	return c.policy.String()
}
