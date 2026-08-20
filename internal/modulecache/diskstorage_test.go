package modulecache

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDiskStorageLoadMissingFileReturnsEmpty проверяет, что отсутствующий
// gob-файл возвращает пустую cacheFile без ошибки.
func TestDiskStorageLoadMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.gob")
	s := NewDiskStorage(path)

	cf, err := s.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf.Version != 0 {
		t.Fatalf("expected Version=0 for missing file, got %d", cf.Version)
	}
	if cf.Entries != nil {
		t.Fatalf("expected nil Entries for missing file, got %v", cf.Entries)
	}
}

// TestDiskStorageSaveLoadRoundTrip проверяет, что Save → Load возвращает те же данные.
func TestDiskStorageSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.gob")
	s := NewDiskStorage(path)

	original := cacheFile{
		Version:         cacheFileVersion,
		LastRefreshTime: time.Now().Truncate(time.Second),
		Entries: map[string]entry{
			"/path/to/docker.yaml": {
				ModTime: time.Now().Truncate(time.Second),
				Size:    1024,
			},
		},
	}

	if err := s.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != original.Version {
		t.Fatalf("Version: got %d, want %d", loaded.Version, original.Version)
	}
	if !loaded.LastRefreshTime.Equal(original.LastRefreshTime) {
		t.Fatalf("LastRefreshTime: got %v, want %v", loaded.LastRefreshTime, original.LastRefreshTime)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("Entries len: got %d, want 1", len(loaded.Entries))
	}
	e, ok := loaded.Entries["/path/to/docker.yaml"]
	if !ok {
		t.Fatal("expected entry for /path/to/docker.yaml")
	}
	if e.Size != 1024 {
		t.Fatalf("Size: got %d, want 1024", e.Size)
	}
}

// TestDiskStorageSaveOverwritesExisting проверяет, что Save перезаписывает существующий файл.
func TestDiskStorageSaveOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.gob")
	s := NewDiskStorage(path)

	// Первая запись
	cf1 := cacheFile{
		Version: cacheFileVersion,
		Entries: map[string]entry{"a": {Size: 1}},
	}
	if err := s.Save(cf1); err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Вторая запись (перезапись)
	cf2 := cacheFile{
		Version: cacheFileVersion,
		Entries: map[string]entry{"b": {Size: 2}},
	}
	if err := s.Save(cf2); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}
	if _, ok := loaded.Entries["a"]; ok {
		t.Fatal("expected old entry 'a' to be overwritten, but it exists")
	}
	if _, ok := loaded.Entries["b"]; !ok {
		t.Fatal("expected new entry 'b' to exist after overwrite")
	}
}

// TestDiskStorageLoadCorruptedReturnsError проверяет, что повреждённый файл — error.
func TestDiskStorageLoadCorruptedReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.gob")
	if err := os.WriteFile(path, []byte("not a gob file"), 0o644); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}

	s := NewDiskStorage(path)
	_, err := s.Load()
	if err == nil {
		t.Fatal("expected error for corrupted file, got nil")
	}
}

// TestDiskStorageSaveCreatesParentDir проверяет, что Save создаёт родительский каталог.
func TestDiskStorageSaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "cache.gob")
	s := NewDiskStorage(path)

	cf := cacheFile{Version: cacheFileVersion, Entries: map[string]entry{}}
	if err := s.Save(cf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

// TestDiskStorageLoadRejectsIncompatibleVersion проверяет, что файл с версией
// из будущего (от новой версии futi) отклоняется при Load. Start воспринимает
// это как ошибку и rebuild-ит кэш с пустого состояния (спека, раздел 2, 10).
func TestDiskStorageLoadRejectsIncompatibleVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.gob")

	// Записываем cacheFile с версией из будущего напрямую через gob.
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	future := cacheFile{
		Version: cacheFileVersion + 1,
		Entries: map[string]entry{"/path": {Size: 100}},
	}
	if err := gob.NewEncoder(f).Encode(future); err != nil {
		t.Fatalf("encode: %v", err)
	}
	f.Close()

	s := NewDiskStorage(path)
	_, err = s.Load()
	if err == nil {
		t.Fatal("expected error for incompatible version, got nil")
	}
}

// TestDiskStorageLoadAcceptsCurrentVersion проверяет, что файл с текущей
// версией загружается без ошибки (sanity check для нового version check).
func TestDiskStorageLoadAcceptsCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.gob")
	s := NewDiskStorage(path)

	original := cacheFile{
		Version:         cacheFileVersion,
		LastRefreshTime: time.Now().Truncate(time.Second),
		Entries:         map[string]entry{"/path": {Size: 100}},
	}
	if err := s.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != cacheFileVersion {
		t.Fatalf("expected Version=%d, got %d", cacheFileVersion, loaded.Version)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}
}
