// Внимание: package-level doc находится в cache.go (canonical).
// Этот файл содержит Storage interface, entry/cacheFile типы и MemoryStorage.

package modulecache

import (
	"time"

	"futi/internal/execmodule"
)

// entry — одна запись кэша: модуль + метаданные для инвалидации.
//
// Инвалидация по mtime+size используется при Refresh (дешёвое сравнение,
// без чтения содержимого). Hash — SHA-256 содержимого манифеста: служит
// «замком» для строгой проверки при исполнении (см. Cache.LoadModuleVerified).
// Запись без Hash (старый формат кэша) считается невалидной для строгой
// проверки и перечитывается с диска.
type entry struct {
	Module  *execmodule.Module
	ModTime time.Time // mtime файла манифеста
	Size    int64     // size файла манифеста
	Hash    []byte    // SHA-256 содержимого манифеста (nil = не проверено/старый кэш)
}

// cacheFile — формат на диске (gob-сериализуемый).
type cacheFile struct {
	Version         int              // версионирование схемы; текущая = 2
	LastRefreshTime time.Time        // для every_t_min политики
	Entries         map[string]entry // только persistent (глобальные) модули
	BinaryID        string           // идентичность бинаря futi, записавшего файл (см. DiskStorage)
}

// Текущая версия схемы cacheFile. При несовпадении — rebuild с пустого состояния.
// Схема Module и правила Validate покрываются идентичностью бинаря
// (cacheFile.BinaryID), поэтому поднимать версию ради них не нужно. Ручной
// подъём нужен только при несовместимом изменении формата самого cacheFile,
// которое gob не переварит.
const cacheFileVersion = 2

// MemoryStorage — no-op хранилище: Load возвращает пустую cacheFile, Save ничего не делает.
// Используется как fallback когда persistent кэш недоступен (нет home dir для cache path),
// и как заготовка для будущего HTTP server (ROADMAP) — long-running процесс с in-memory кэшем.
type MemoryStorage struct{}

// Load возвращает пустую cacheFile — нет persistent состояния.
func (MemoryStorage) Load() (cacheFile, error) { return cacheFile{}, nil }

// Save — no-op. Ничего не сохраняет.
func (MemoryStorage) Save(cacheFile) error { return nil }

// Clear — no-op: нет persistent состояния.
func (MemoryStorage) Clear() error { return nil }

// Storage — абстракция над persistent/in-memory хранилищем кэша.
//
// Реализации:
//   - DiskStorage — gob-файл на диске (CLI).
//   - MemoryStorage — no-op для HTTP (будущее, не реализован).
type Storage interface {
	// Load загружает состояние. Возвращает пустую cacheFile (без ошибки), если
	// storage пуст (файл отсутствует). Возвращает error при повреждении или
	// несовместимой версии — вызывающий решает: rebuild.
	Load() (cacheFile, error)

	// Save сохраняет cacheFile целиком (Version, LastRefreshTime, Entries).
	// Атомарная запись (temp + rename) для DiskStorage.
	Save(cf cacheFile) error

	// Clear удаляет persistent состояние (файл хранилища).
	// In-memory записи не затрагивает — ими управляет Cache.
	Clear() error
}
