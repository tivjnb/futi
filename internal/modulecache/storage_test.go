package modulecache

import (
	"testing"
	"time"
)

// TestMemoryStorageLoadReturnsEmpty проверяет, что Load возвращает пустую
// cacheFile без ошибки — нет persistent состояния.
func TestMemoryStorageLoadReturnsEmpty(t *testing.T) {
	s := MemoryStorage{}
	cf, err := s.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf.Version != 0 {
		t.Fatalf("expected Version=0, got %d", cf.Version)
	}
	if cf.Entries != nil {
		t.Fatalf("expected nil Entries, got %v", cf.Entries)
	}
	if !cf.LastRefreshTime.IsZero() {
		t.Fatalf("expected zero LastRefreshTime, got %v", cf.LastRefreshTime)
	}
}

// TestMemoryStorageSaveIsNoOp проверяет, что Save ничего не сохраняет —
// последующий Load возвращает ту же пустую cacheFile.
func TestMemoryStorageSaveIsNoOp(t *testing.T) {
	s := MemoryStorage{}
	cf := cacheFile{
		Version:         cacheFileVersion,
		LastRefreshTime: time.Now(),
		Entries:         map[string]entry{"/path": {Size: 100}},
	}
	if err := s.Save(cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, _ := s.Load()
	if loaded.Version != 0 {
		t.Fatalf("expected Version=0 after Save (no-op), got %d", loaded.Version)
	}
	if loaded.Entries != nil {
		t.Fatalf("expected nil Entries after Save (no-op), got %v", loaded.Entries)
	}
}
