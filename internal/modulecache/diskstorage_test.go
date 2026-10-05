package modulecache

import (
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// TestDiskStorageLoadIgnoresIncompatibleVersion проверяет, что файл с версией
// из будущего (от новой версии futi) при Load даёт пустой результат без ошибки:
// кэш тихо перестраивается с пустого состояния.
func TestDiskStorageLoadIgnoresIncompatibleVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.gob")

	// Записываем cacheFile с версией из будущего напрямую через gob.
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	future := cacheFile{
		Version:  cacheFileVersion + 1,
		BinaryID: "a",
		Entries:  map[string]entry{"/path": {Size: 100}},
	}
	if err := gob.NewEncoder(f).Encode(future); err != nil {
		t.Fatalf("encode: %v", err)
	}
	f.Close()

	cf, err := newDiskStorageWithID(path, "a").Load()
	if err != nil {
		t.Fatalf("Load: unexpected error %v, want silent rebuild", err)
	}
	if len(cf.Entries) != 0 {
		t.Fatalf("Load future version: got %d entries, want 0", len(cf.Entries))
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

// newDiskStorageWithID — DiskStorage с заданной идентичностью бинаря (для тестов).
func newDiskStorageWithID(path, id string) *DiskStorage {
	s := NewDiskStorage(path)
	s.binaryID = id
	return s
}

func sampleCacheFile() cacheFile {
	return cacheFile{
		Version: cacheFileVersion,
		Entries: map[string]entry{"/m.yaml": {Size: 7}},
	}
}

// Файл, сохранённый другой сборкой futi, не используется: пустой cacheFile, без ошибки.
func TestDiskStorageBinaryIDMismatchIgnoresCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.gob")
	if err := newDiskStorageWithID(path, "a").Save(sampleCacheFile()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cf, err := newDiskStorageWithID(path, "b").Load()
	if err != nil {
		t.Fatalf("Load: unexpected error %v", err)
	}
	if len(cf.Entries) != 0 {
		t.Fatalf("Load with id b after Save with id a: got %d entries, want 0", len(cf.Entries))
	}
}

func TestDiskStorageBinaryIDMatchKeepsCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.gob")
	if err := newDiskStorageWithID(path, "a").Save(sampleCacheFile()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cf, err := newDiskStorageWithID(path, "a").Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cf.Entries) != 1 || cf.Entries["/m.yaml"].Size != 7 {
		t.Fatalf("Load with same id: got %+v, want 1 entry with Size 7", cf.Entries)
	}
}

// Неопределённая идентичность (пустая) не совпадает даже с пустой записанной.
func TestDiskStorageEmptyBinaryIDNeverMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.gob")
	if err := newDiskStorageWithID(path, "").Save(sampleCacheFile()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cf, err := newDiskStorageWithID(path, "").Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cf.Entries) != 0 {
		t.Fatalf("Load with empty ids: got %d entries, want 0", len(cf.Entries))
	}
}

// Файл старого формата v2 (без поля BinaryID) считается устаревшим, без ошибки.
func TestDiskStorageLegacyFileWithoutBinaryID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.gob")
	legacy := struct {
		Version         int
		LastRefreshTime time.Time
		Entries         map[string]entry
	}{Version: cacheFileVersion, Entries: map[string]entry{"/m.yaml": {Size: 7}}}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := gob.NewEncoder(f).Encode(legacy); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	cf, err := newDiskStorageWithID(path, "a").Load()
	if err != nil {
		t.Fatalf("Load: unexpected error %v", err)
	}
	if len(cf.Entries) != 0 {
		t.Fatalf("Load legacy file: got %d entries, want 0", len(cf.Entries))
	}
}

func TestExecutableIDNonEmpty(t *testing.T) {
	first := executableID()
	if first == "" {
		t.Fatal("executableID() is empty in test process")
	}
	if second := executableID(); second != first {
		t.Fatalf("executableID() not stable: %q then %q", first, second)
	}
}

// Идентичность включает реальный путь: на Nix/Guix mtime у всех файлов равен 1,
// а cp -p сохраняет mtime, поэтому mtime и размер одинаковы у разных бинарей.
func TestFileIdentityDependsOnPath(t *testing.T) {
	dir := t.TempDir()
	mtime := time.Unix(1, 0)
	var ids []string
	for _, name := range []string{"futi-a", "futi-b"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("same"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		ids = append(ids, fileIdentity(p, filepath.EvalSymlinks))
	}
	if ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("identity for different paths with equal mtime/size: %q vs %q, want different non-empty", ids[0], ids[1])
	}
}

// Симлинк разрешается: идентичность симлинка и его цели совпадает.
func TestFileIdentityResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	a, b := fileIdentity(link, filepath.EvalSymlinks), fileIdentity(target, filepath.EvalSymlinks)
	if a == "" {
		t.Fatal("identity via symlink is empty")
	}
	if a != b {
		t.Fatalf("identity via symlink %q != identity of target %q", a, b)
	}
}

// Ошибка EvalSymlinks не обнуляет идентичность: берётся Stat неразрешённого пути.
func TestFileIdentityFallbackWhenEvalSymlinksFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "futi")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	failing := func(string) (string, error) { return "", errors.New("eval failed") }
	id := fileIdentity(p, failing)
	if id == "" {
		t.Fatal("identity is empty after EvalSymlinks failure, want fallback to Stat of unresolved path")
	}
	if !strings.Contains(id, p) {
		t.Fatalf("identity %q does not contain unresolved path %q", id, p)
	}
}

func TestFileIdentityEmptyWhenStatFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing")
	if id := fileIdentity(p, filepath.EvalSymlinks); id != "" {
		t.Fatalf("identity of missing file = %q, want empty", id)
	}
}

// Несовпадение версии схемы — штатная тихая перестройка, как и чужая идентичность.
func TestDiskStorageLoadOldVersionIsSilentRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.gob")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	old := cacheFile{Version: 2, BinaryID: "a", Entries: map[string]entry{"/m.yaml": {Size: 7}}}
	if err := gob.NewEncoder(f).Encode(old); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	cf, err := newDiskStorageWithID(path, "a").Load()
	if err != nil {
		t.Fatalf("Load version 2: unexpected error %v", err)
	}
	if len(cf.Entries) != 0 {
		t.Fatalf("Load version 2: got %d entries, want 0", len(cf.Entries))
	}
}
