package modulecache

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
)

// DiskStorage — persistent хранилище кэша в gob-файле на диске.
//
// Путь: обычно ~/.cache/futi/modulecache.gob (через os.UserCacheDir()).
// Save использует атомарную запись: temp file + rename — предотвращает
// частичную запись при краше процесса.
type DiskStorage struct {
	path string
}

// NewDiskStorage создаёт DiskStorage для заданного пути.
// Родительский каталог создаётся при Save (если отсутствует).
func NewDiskStorage(path string) *DiskStorage {
	return &DiskStorage{path: path}
}

// Load загружает cacheFile из gob-файла.
// Если файл отсутствует — возвращает пустую cacheFile без ошибки.
// Если файл повреждён — возвращает error (вызывающий решает: rebuild).
// Если версия схемы не совпадает с cacheFileVersion — возвращает error,
// чтобы Start перестроил кэш с пустого состояния (спека, раздел 2, 10).
func (d *DiskStorage) Load() (cacheFile, error) {
	f, err := os.Open(d.path)
	if err != nil {
		if os.IsNotExist(err) {
			return cacheFile{}, nil
		}
		return cacheFile{}, fmt.Errorf("modulecache: open cache file %q: %w", d.path, err)
	}
	defer f.Close()

	var cf cacheFile
	if err := gob.NewDecoder(f).Decode(&cf); err != nil {
		return cacheFile{}, fmt.Errorf("modulecache: decode cache file %q: %w", d.path, err)
	}
	// Проверка версии схемы: при несовпадении (новая версия futi с другим
	// форматом) — error, вызывающий (Cache.Start) rebuild-ит с пустого состояния.
	if cf.Version != cacheFileVersion {
		return cacheFile{}, fmt.Errorf("modulecache: cache file %q version mismatch: got %d, want %d (rebuilding)", d.path, cf.Version, cacheFileVersion)
	}
	return cf, nil
}

// Save атомарно записывает cacheFile в gob-файл.
// Создаёт родительский каталог при необходимости.
// Атомарность: пишет во временный файл, затем rename — предотвращает
// частичную запись при краше.
func (d *DiskStorage) Save(cf cacheFile) error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return fmt.Errorf("modulecache: create cache dir %q: %w", filepath.Dir(d.path), err)
	}

	// Пишем во временный файл в том же каталоге (rename атомарен только
	// в пределах одной файловой системы).
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".modulecache-*.tmp")
	if err != nil {
		return fmt.Errorf("modulecache: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // cleanup если rename не произошёл

	if err := gob.NewEncoder(tmp).Encode(cf); err != nil {
		tmp.Close()
		return fmt.Errorf("modulecache: encode cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("modulecache: close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, d.path); err != nil {
		return fmt.Errorf("modulecache: rename temp to %q: %w", d.path, err)
	}
	return nil
}
