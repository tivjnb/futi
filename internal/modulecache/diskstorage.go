package modulecache

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// DiskStorage — persistent хранилище кэша в gob-файле на диске.
//
// Путь: обычно ~/.cache/futi/modulecache.gob (через os.UserCacheDir()).
// Save использует атомарную запись: temp file + rename — предотвращает
// частичную запись при краше процесса.
//
// Файл привязан к идентичности бинаря futi, записавшего его: после
// обновления или пересборки futi Module и правила Validate могли измениться,
// поэтому чужой кэш не используется.
type DiskStorage struct {
	path     string
	binaryID string // идентичность текущего бинаря; "" = не определена
}

var (
	executableIDOnce  sync.Once
	executableIDValue string
)

// executableID возвращает идентичность исполняемого файла текущего процесса
// (см. fileIdentity) или "" при любой ошибке. Вычисляется один раз на процесс.
func executableID() string {
	executableIDOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		executableIDValue = fileIdentity(exe, filepath.EvalSymlinks)
	})
	return executableIDValue
}

// fileIdentity строит идентичность файла: реальный путь (после evalSymlinks) +
// mtime в наносекундах + размер. Путь нужен, потому что на Nix/Guix у всех
// файлов mtime=1, а cp -p / install -p / rsync -t сохраняют mtime: новый бинарь
// того же размера иначе выдавал бы старую идентичность. Если evalSymlinks
// вернул ошибку — используется неразрешённый путь. "" — только если не удался Stat.
func fileIdentity(exe string, evalSymlinks func(string) (string, error)) string {
	if resolved, err := evalSymlinks(exe); err == nil {
		exe = resolved
	}
	info, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return exe + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)
}

// NewDiskStorage создаёт DiskStorage для заданного пути.
// Родительский каталог создаётся при Save (если отсутствует).
func NewDiskStorage(path string) *DiskStorage {
	return &DiskStorage{path: path, binaryID: executableID()}
}

// Load загружает cacheFile из gob-файла.
// Если файл отсутствует — возвращает пустую cacheFile без ошибки.
// Если файл повреждён — возвращает error (вызывающий решает: rebuild).
// Если версия схемы не совпадает с cacheFileVersion, а также если идентичность
// бинаря не определена или не совпадает с записанной (в том числе у файла
// старого формата без неё) — возвращает пустую cacheFile без ошибки и без
// лога: это штатно после каждого обновления futi (кэш перестраивается с
// пустого состояния; строка в stderr попала бы в вывод futi __complete).
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
	// Другая версия схемы или чужая идентичность бинаря — ожидаемо после
	// обновления/отката futi: тихая перестройка с пустого состояния.
	if cf.Version != cacheFileVersion || d.binaryID == "" || cf.BinaryID != d.binaryID {
		return cacheFile{}, nil
	}
	return cf, nil
}

// Save атомарно записывает cacheFile в gob-файл.
// Создаёт родительский каталог при необходимости.
// Атомарность: пишет во временный файл, затем rename — предотвращает
// частичную запись при краше.
func (d *DiskStorage) Save(cf cacheFile) error {
	cf.BinaryID = d.binaryID
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
	// #36: fsync перед close — данные попадают на диск, а не остаются в page cache.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("modulecache: sync cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("modulecache: close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, d.path); err != nil {
		return fmt.Errorf("modulecache: rename temp to %q: %w", d.path, err)
	}
	return nil
}

// Clear удаляет gob-файл с диска. Отсутствие файла — не ошибка.
func (d *DiskStorage) Clear() error {
	if err := os.Remove(d.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("modulecache: clear %q: %w", d.path, err)
	}
	return nil
}
