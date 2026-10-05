package modulecache

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"futi/internal/discovery"
	"futi/internal/execmodule"
)

// Сентинельные ошибки для тестов обработки ошибок.
var (
	errCorruptedForTest = errors.New("test: corrupted cache file")
	errSaveForTest      = errors.New("test: save failed")
)

// fakeStorage — in-memory реализация Storage для тестов.
// Записывает Save-вызовы и возвращает заданные данные при Load.
// Потокобезопасен (Save вызывается вне лока Cache — #14).
type fakeStorage struct {
	mu       sync.Mutex
	loadData cacheFile
	loadErr  error
	saveErr  error
	saves    int
	lastSave cacheFile
}

func (f *fakeStorage) Load() (cacheFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return cacheFile{}, f.loadErr
	}
	return f.loadData, nil
}

func (f *fakeStorage) Save(cf cacheFile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	f.lastSave = cf
	if f.saveErr != nil {
		return f.saveErr
	}
	f.loadData = cf
	return nil
}

func (f *fakeStorage) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadData = cacheFile{}
	return nil
}

func newTestCache() *Cache {
	return New(&fakeStorage{}, AlwaysPolicy{})
}

// --- LoadModule (существующие тесты, обновлённые под новый API) ---

// TestCacheReturnsSamePointerForSamePath проверяет, что повторный запрос
// того же манифеста возвращает тот же *Module (cache hit).
func TestCacheReturnsSamePointerForSamePath(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	writeFile(t, manifestPath, `
name: echo_mod
cmd: "echo {{.args}}"
`)

	cache := newTestCache()
	mod1, err := cache.LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	mod2, err := cache.LoadModule(manifestPath, dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if mod1 != mod2 {
		t.Fatalf("expected same *Module pointer (cache hit), got %p and %p", mod1, mod2)
	}
}

// TestCacheReturnsDifferentPointersForDifferentPaths проверяет, что разные
// манифесты дают разные *Module с корректным содержимым.
func TestCacheReturnsDifferentPointersForDifferentPaths(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.yaml")
	path2 := filepath.Join(dir, "b.yaml")
	writeFile(t, path1, `name: a`)
	writeFile(t, path2, `name: b`)

	cache := newTestCache()
	mod1, err := cache.LoadModule(path1, dir)
	if err != nil {
		t.Fatalf("load a: %v", err)
	}
	mod2, err := cache.LoadModule(path2, dir)
	if err != nil {
		t.Fatalf("load b: %v", err)
	}
	if mod1 == mod2 {
		t.Fatal("expected different *Module pointers for different paths")
	}
	if mod1.Name != "a" {
		t.Fatalf("expected mod1.Name=a, got %q", mod1.Name)
	}
	if mod2.Name != "b" {
		t.Fatalf("expected mod2.Name=b, got %q", mod2.Name)
	}
}

// TestCacheReturnsErrorForMissingFile проверяет, что несуществующий манифест
// возвращает ошибку.
func TestCacheReturnsErrorForMissingFile(t *testing.T) {
	dir := t.TempDir()
	cache := newTestCache()
	_, err := cache.LoadModule(filepath.Join(dir, "nonexistent.yaml"), dir)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestCacheDoesNotCacheError проверяет, что неудачная загрузка не кэшируется.
func TestCacheDoesNotCacheError(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "late.yaml")
	cache := newTestCache()

	_, err := cache.LoadModule(missingPath, dir)
	if err == nil {
		t.Fatal("expected first load to fail for missing file")
	}

	writeFile(t, missingPath, `name: created_later`)

	mod, err := cache.LoadModule(missingPath, dir)
	if err != nil {
		t.Fatalf("expected second load to succeed after file created, got: %v", err)
	}
	if mod.Name != "created_later" {
		t.Fatalf("expected name=created_later, got %q", mod.Name)
	}
}

// --- Local vs persistent разделение ---

// TestCacheLoadModuleDoesNotAddToPersistentPaths проверяет, что LoadModule
// (cache miss) добавляет модуль в in-memory entries, но НЕ в persistentPaths.
// Локальные модули не сохраняются на диск.
func TestCacheLoadModuleDoesNotAddToPersistentPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.yaml")
	writeFile(t, path, `name: local_mod`)

	cache := newTestCache()
	if _, err := cache.LoadModule(path, dir); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.entries[path]; !ok {
		t.Fatal("expected entry in in-memory cache")
	}
	if cache.persistentPaths[path] {
		t.Fatal("expected path NOT in persistentPaths (local module)")
	}
}

// TestCacheBuildCacheFileOnlyPersistsGlobalModules проверяет, что buildCacheFile
// сохраняет только persistent (глобальные) модули, не локальные.
func TestCacheBuildCacheFileOnlyPersistsGlobalModules(t *testing.T) {
	storage := &fakeStorage{}
	cache := New(storage, AlwaysPolicy{})

	// Локальный модуль (через LoadModule — не persistent)
	dir := t.TempDir()
	localPath := filepath.Join(dir, "local.yaml")
	writeFile(t, localPath, `name: local_mod`)
	if _, err := cache.LoadModule(localPath, dir); err != nil {
		t.Fatalf("LoadModule local: %v", err)
	}

	// Глобальный модуль (добавляем в persistentPaths вручную для теста)
	globalPath := filepath.Join(dir, "global.yaml")
	writeFile(t, globalPath, `name: global_mod`)
	cache.mu.Lock()
	cache.entries[globalPath] = entry{
		Module:  &execmodule.Module{Name: "global_mod"},
		ModTime: time.Now(),
		Size:    100,
	}
	cache.persistentPaths[globalPath] = true
	cache.lastRefresh = time.Now()
	cf := cache.buildCacheFile()
	cache.mu.Unlock()

	if err := storage.Save(cf); err != nil {
		t.Fatalf("Save: %v", err)
	}

	saved := storage.lastSave
	if len(saved.Entries) != 1 {
		t.Fatalf("expected 1 persistent entry, got %d", len(saved.Entries))
	}
	if _, ok := saved.Entries[globalPath]; !ok {
		t.Fatal("expected globalPath in saved entries")
	}
	if _, ok := saved.Entries[localPath]; ok {
		t.Fatal("expected localPath NOT in saved entries")
	}
}

// --- Start ---

// TestCacheStartLoadsFromStorage проверяет, что Start загружает entries из storage.
func TestCacheStartLoadsFromStorage(t *testing.T) {
	globalPath := "/fake/global.yaml"
	storage := &fakeStorage{
		loadData: cacheFile{
			Version:         cacheFileVersion,
			LastRefreshTime: time.Now().Add(-1 * time.Hour),
			Entries: map[string]entry{
				globalPath: {
					Module:  &execmodule.Module{Name: "cached_global"},
					ModTime: time.Now().Add(-2 * time.Hour),
					Size:    200,
				},
			},
		},
	}

	// ForceOnly — чтобы не запускать Refresh (тестируем только Load)
	cache := New(storage, ForceOnlyPolicy{})

	// Создаём пустой source (Refresh не вызовется при ForceOnly)
	src := discovery.NewGlobalSource(t.TempDir())
	if err := cache.Start(src); err != nil {
		t.Fatalf("Start: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	e, ok := cache.entries[globalPath]
	if !ok {
		t.Fatal("expected entry loaded from storage")
	}
	if e.Module == nil || e.Module.Name != "cached_global" {
		t.Fatalf("expected cached_global, got %v", e.Module)
	}
	if !cache.persistentPaths[globalPath] {
		t.Fatal("expected globalPath in persistentPaths")
	}
}

// TestCacheStartWithAlwaysPolicyRefreshes проверяет, что Start с AlwaysPolicy
// запускает Refresh.
func TestCacheStartWithAlwaysPolicyRefreshes(t *testing.T) {
	// Создаём реальный source с модулем
	dir := t.TempDir()
	modPath := filepath.Join(dir, "docker.yaml")
	writeFile(t, modPath, `name: docker
cmd: "echo docker"`)

	src := discovery.NewGlobalSource(dir)
	storage := &fakeStorage{}
	cache := New(storage, AlwaysPolicy{})

	if err := cache.Start(src); err != nil {
		t.Fatalf("Start: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.entries[modPath]; !ok {
		t.Fatal("expected module loaded by Refresh during Start")
	}
	if !cache.persistentPaths[modPath] {
		t.Fatal("expected modPath in persistentPaths after Refresh")
	}
	if cache.lastRefresh.IsZero() {
		t.Fatal("expected lastRefresh to be set after Start with AlwaysPolicy")
	}
}

// TestCacheStartWithForceOnlyDoesNotRefresh проверяет, что Start с ForceOnlyPolicy
// не запускает Refresh.
func TestCacheStartWithForceOnlyDoesNotRefresh(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "docker.yaml")
	writeFile(t, modPath, `name: docker`)

	src := discovery.NewGlobalSource(dir)
	storage := &fakeStorage{}
	cache := New(storage, ForceOnlyPolicy{})

	if err := cache.Start(src); err != nil {
		t.Fatalf("Start: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.entries[modPath]; ok {
		t.Fatal("expected module NOT loaded (ForceOnly, no refresh)")
	}
}

// TestCacheStartRebuildsOnCorruptedStorage проверяет, что при ошибке Load
// кэш rebuild-ится с пустого состояния и продолжает работу.
func TestCacheStartRebuildsOnCorruptedStorage(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)

	src := discovery.NewGlobalSource(dir)
	storage := &fakeStorage{
		loadErr: errCorruptedForTest,
	}
	cache := New(storage, AlwaysPolicy{}) // AlwaysPolicy → Refresh заполнит заново

	if err := cache.Start(src); err != nil {
		t.Fatalf("Start should not fail on corrupted storage: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// После rebuild + Refresh модуль должен быть загружен
	if _, ok := cache.entries[modPath]; !ok {
		t.Fatal("expected module loaded after rebuild + Refresh")
	}
}

// --- Refresh ---

// TestCacheRefreshReadsNewModules проверяет, что Refresh загружает модули из source.
func TestCacheRefreshReadsNewModules(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "deploy.yaml")
	writeFile(t, modPath, `name: deploy
cmd: "echo deploy"`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected Total=1, got %d", result.Total)
	}
	if result.Updated != 1 {
		t.Fatalf("expected Updated=1, got %d", result.Updated)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	e, ok := cache.entries[modPath]
	if !ok {
		t.Fatal("expected module in entries after Refresh")
	}
	if e.Module == nil || e.Module.Name != "deploy" {
		t.Fatalf("expected deploy module, got %v", e.Module)
	}
	if !cache.persistentPaths[modPath] {
		t.Fatal("expected modPath in persistentPaths")
	}
}

// TestCacheRefreshSkipsUnchangedModules проверяет, что Refresh не перечитывает
// неизменившиеся модули (mtime+size совпадают).
func TestCacheRefreshSkipsUnchangedModules(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)

	info, _ := os.Stat(modPath)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	// Предзаполняем кэш актуальными mtime+size
	cache.mu.Lock()
	cache.entries[modPath] = entry{
		Module:  &execmodule.Module{Name: "cached_app"},
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}
	cache.persistentPaths[modPath] = true
	cache.mu.Unlock()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected Total=1, got %d", result.Total)
	}
	if result.Updated != 0 {
		t.Fatalf("expected Updated=0 (unchanged), got %d", result.Updated)
	}

	// Тот же pointer — модуль не перечитывался
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries[modPath].Module.Name != "cached_app" {
		t.Fatal("expected cached module to be preserved (not re-read)")
	}
}

// TestCacheRefreshRereadsChangedModules проверяет, что Refresh перечитывает
// изменившиеся модули (mtime или size отличаются).
func TestCacheRefreshRereadsChangedModules(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app
cmd: "echo v1"`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	// Предзаполняем устаревшими mtime+size
	cache.mu.Lock()
	cache.entries[modPath] = entry{
		Module:  &execmodule.Module{Name: "old_app"},
		ModTime: time.Now().Add(-1 * time.Hour), // старое время
		Size:    999,                            // не совпадает
	}
	cache.persistentPaths[modPath] = true
	cache.mu.Unlock()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected Updated=1 (changed), got %d", result.Updated)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries[modPath].Module.Name != "app" {
		t.Fatal("expected module re-read from disk")
	}
}

// TestCacheRefreshUpdatesLastRefreshTime проверяет, что Refresh обновляет lastRefresh.
func TestCacheRefreshUpdatesLastRefreshTime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), `name: app`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	before := time.Now()
	if _, err := cache.Refresh(src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.lastRefresh.Before(before) {
		t.Fatal("expected lastRefresh to be updated")
	}
}

// TestCacheRefreshSavesToStorage проверяет, что Refresh сохраняет кэш на диск.
func TestCacheRefreshSavesToStorage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), `name: app`)

	src := discovery.NewGlobalSource(dir)
	storage := &fakeStorage{}
	cache := New(storage, AlwaysPolicy{})

	if _, err := cache.Refresh(src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if storage.saves != 1 {
		t.Fatalf("expected 1 Save call, got %d", storage.saves)
	}
	if storage.lastSave.Version != cacheFileVersion {
		t.Fatalf("expected Version=%d, got %d", cacheFileVersion, storage.lastSave.Version)
	}
}

// TestCacheRefreshEmptySource проверяет, что Refresh с пустым source
// не падает и возвращает нули.
func TestCacheRefreshEmptySource(t *testing.T) {
	src := discovery.NewGlobalSource(t.TempDir())
	cache := newTestCache()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Total != 0 || result.Updated != 0 {
		t.Fatalf("expected zeros for empty source, got %+v", result)
	}
}

// --- Save error handling ---

// TestCacheRefreshSaveErrorIsNonFatal проверяет, что ошибка Save не фатальна —
// in-memory кэш остаётся актуальным.
func TestCacheRefreshSaveErrorIsNonFatal(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)

	src := discovery.NewGlobalSource(dir)
	storage := &fakeStorage{saveErr: errSaveForTest}
	cache := New(storage, AlwaysPolicy{})

	// Подавляем лог Save-ошибки для чистоты вывода теста.
	oldOut := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldOut)

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh should not fail on Save error: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected Updated=1, got %d", result.Updated)
	}

	// In-memory кэш актуален несмотря на ошибку Save
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.entries[modPath]; !ok {
		t.Fatal("expected module in entries despite Save error")
	}
}

// TestCacheRefreshLogsSaveError проверяет, что ошибка Save логируется как warning
// через log-пакет, а не проглатывается молча. Refresh при этом не падает.
// Спека, раздел 6: "Ошибка записи при Save → лог warning, продолжение работы".
func TestCacheRefreshLogsSaveError(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)

	src := discovery.NewGlobalSource(dir)
	storage := &fakeStorage{saveErr: errSaveForTest}
	cache := New(storage, AlwaysPolicy{})

	// Перехватываем вывод log-пакета.
	var buf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOut)

	_, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh should not fail on Save error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, errSaveForTest.Error()) {
		t.Fatalf("expected log to mention Save error %q, got: %q", errSaveForTest.Error(), logOutput)
	}
	if !strings.Contains(logOutput, "save") && !strings.Contains(logOutput, "Save") {
		t.Fatalf("expected log to mention 'save', got: %q", logOutput)
	}
}

// TestCacheStartLogsSaveError проверяет, что ошибка Save в Start (когда policy
// не требует Refresh) логируется, а не проглатывается.
func TestCacheStartLogsSaveError(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)

	src := discovery.NewGlobalSource(dir)
	storage := &fakeStorage{saveErr: errSaveForTest}
	cache := New(storage, ForceOnlyPolicy{}) // ForceOnly → saveToStorage без Refresh

	var buf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOut)

	if err := cache.Start(src); err != nil {
		t.Fatalf("Start should not fail on Save error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, errSaveForTest.Error()) {
		t.Fatalf("expected log to mention Save error %q, got: %q", errSaveForTest.Error(), logOutput)
	}
	_ = modPath
}

// TestCacheStartRebuildsAndOverwritesCorruptedDiskFile проверяет, что при
// повреждённом gob-файле Start rebuild-ит кэш и перезаписывает файл валидным
// cacheFile. Спека подразумевает перезапись повреждённого файла, а не только
// in-memory rebuild.
func TestCacheStartRebuildsAndOverwritesCorruptedDiskFile(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "modulecache.gob")

	// Записываем мусор в gob-файл.
	if err := os.WriteFile(cachePath, []byte("not a gob file"), 0o644); err != nil {
		t.Fatalf("write corrupted: %v", err)
	}

	// Source с одним модулем.
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)
	src := discovery.NewGlobalSource(dir)

	storage := NewDiskStorage(cachePath)
	cache := New(storage, AlwaysPolicy{}) // AlwaysPolicy → Refresh rebuilds

	if err := cache.Start(src); err != nil {
		t.Fatalf("Start should not fail on corrupted storage: %v", err)
	}

	// Файл должен быть перезаписан валидным cacheFile.
	loaded, err := storage.Load()
	if err != nil {
		t.Fatalf("Load after rebuild should succeed: %v", err)
	}
	if loaded.Version != cacheFileVersion {
		t.Fatalf("expected Version=%d after rebuild, got %d", cacheFileVersion, loaded.Version)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry after rebuild, got %d", len(loaded.Entries))
	}
	if _, ok := loaded.Entries[modPath]; !ok {
		t.Fatal("expected modPath in loaded entries after rebuild")
	}
}

// --- Group 2: Refresh correctness (#7, #8, #9) ---

// suppressLog — хелпер для тестов, которые вызывают log.Printf через Refresh
// (skipped modules, save errors). Возвращает restore-функцию.
func suppressLog(t *testing.T) func() {
	t.Helper()
	oldOut := log.Writer()
	log.SetOutput(io.Discard)
	return func() { log.SetOutput(oldOut) }
}

// TestCacheRefreshCountsSkippedModules проверяет, что модули с ошибкой
// LoadModule подсчитываются в result.Skipped, а не молча проглатываются.
// Спека: RefreshResult должен содержать счётчик skipped.
func TestCacheRefreshCountsSkippedModules(t *testing.T) {
	dir := t.TempDir()
	// Валидный модуль
	writeFile(t, filepath.Join(dir, "good.yaml"), `name: good`)
	// Невалидный модуль (команда без cmd и commands → Validate error → LoadModule fails)
	writeFile(t, filepath.Join(dir, "bad.yaml"), `name: bad
commands:
  empty: {}`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()
	restore := suppressLog(t)
	defer restore()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected Total=2, got %d", result.Total)
	}
	if result.Updated != 1 {
		t.Fatalf("expected Updated=1 (good), got %d", result.Updated)
	}
	if result.Skipped != 1 {
		t.Fatalf("expected Skipped=1 (bad), got %d", result.Skipped)
	}
}

// TestCacheRefreshDoesNotUpdateLastRefreshWhenAllSkipped проверяет, что
// lastRefresh НЕ обновляется, если все модули пропущены (полная неудача).
// При every_t_min это позволяет следующему запуску сразу retry.
func TestCacheRefreshDoesNotUpdateLastRefreshWhenAllSkipped(t *testing.T) {
	dir := t.TempDir()
	// Все модули невалидные
	writeFile(t, filepath.Join(dir, "bad.yaml"), `name: bad
commands:
  empty: {}`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	oldRefresh := time.Now().Add(-1 * time.Hour)
	cache.mu.Lock()
	cache.lastRefresh = oldRefresh
	cache.mu.Unlock()

	restore := suppressLog(t)
	defer restore()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Skipped != 1 {
		t.Fatalf("expected Skipped=1, got %d", result.Skipped)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.lastRefresh.Equal(oldRefresh) {
		t.Fatalf("expected lastRefresh unchanged when all skipped, got %v (was %v)", cache.lastRefresh, oldRefresh)
	}
}

// TestCacheRefreshUpdatesLastRefreshOnPartialSuccess проверяет, что lastRefresh
// обновляется, если хотя бы один модуль успешно обработан (даже при частичных
// ошибках). Это не даёт одному битому модулю ломать throttling every_t_min.
func TestCacheRefreshUpdatesLastRefreshOnPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "good.yaml"), `name: good`)
	writeFile(t, filepath.Join(dir, "bad.yaml"), `name: bad
commands:
  empty: {}`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	oldRefresh := time.Now().Add(-1 * time.Hour)
	cache.mu.Lock()
	cache.lastRefresh = oldRefresh
	cache.mu.Unlock()

	restore := suppressLog(t)
	defer restore()

	before := time.Now()
	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Skipped != 1 || result.Updated != 1 {
		t.Fatalf("expected Skipped=1, Updated=1, got Skipped=%d, Updated=%d", result.Skipped, result.Updated)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.lastRefresh.Equal(oldRefresh) {
		t.Fatal("expected lastRefresh to be updated on partial success")
	}
	if cache.lastRefresh.Before(before) {
		t.Fatal("expected lastRefresh to be >= time before Refresh")
	}
}

// TestCacheRefreshCleansUpStaleEntries проверяет, что модули, удалённые из
// source, удаляются из entries и persistentPaths при Refresh. Stale entries
// не накапливаются и не раздули gob-файл.
func TestCacheRefreshCleansUpStaleEntries(t *testing.T) {
	dir := t.TempDir()
	// Только good.yaml существует в source
	goodPath := filepath.Join(dir, "good.yaml")
	writeFile(t, goodPath, `name: good`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	// Предзаполняем кэш устаревшей записью (файл удалён из source)
	stalePath := filepath.Join(dir, "deleted.yaml")
	cache.mu.Lock()
	cache.entries[stalePath] = entry{
		Module:  &execmodule.Module{Name: "deleted"},
		ModTime: time.Now().Add(-1 * time.Hour),
		Size:    100,
	}
	cache.persistentPaths[stalePath] = true
	cache.mu.Unlock()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected Updated=1, got %d", result.Updated)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Stale entry должна быть удалена
	if _, ok := cache.entries[stalePath]; ok {
		t.Fatal("expected stale entry to be removed from entries")
	}
	if cache.persistentPaths[stalePath] {
		t.Fatal("expected stale path to be removed from persistentPaths")
	}
	// Good entry должна быть на месте
	if _, ok := cache.entries[goodPath]; !ok {
		t.Fatal("expected good entry to be present in entries")
	}
	if !cache.persistentPaths[goodPath] {
		t.Fatal("expected good path to be in persistentPaths")
	}
}

// TestCacheRefreshDoesNotRemoveEntriesWithReadErrors проверяет, что модули,
// которые есть в source (discovery нашёл), но прочитать их не удалось
// (временная ошибка, здесь — нет прав на чтение), НЕ удаляются из кэша.
// Cleanup трогает только те, кого больше нет в source.
func TestCacheRefreshDoesNotRemoveEntriesWithReadErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root читает файл без прав на чтение")
	}
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "unreadable.yaml")
	writeFile(t, unreadable, "name: unreadable\n")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	// Предзаполняем кэш устаревшей записью: mtime не совпадёт → перечитывание.
	cache.mu.Lock()
	cache.entries[unreadable] = entry{
		Module:  &execmodule.Module{Name: "cached_unreadable"},
		ModTime: time.Now().Add(-1 * time.Hour),
		Size:    100,
	}
	cache.persistentPaths[unreadable] = true
	cache.mu.Unlock()

	restore := suppressLog(t)
	defer restore()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Total != 1 || result.Skipped != 1 {
		t.Fatalf("Refresh: Total=%d Skipped=%d, want 1 и 1", result.Total, result.Skipped)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// unreadable.yaml в source (seenPaths), но чтение упало → НЕ удаляем.
	if e, ok := cache.entries[unreadable]; !ok || e.Module.Name != "cached_unreadable" {
		t.Fatalf("запись нечитаемого модуля должна сохраниться, got %+v (ok=%v)", e, ok)
	}
	if !cache.persistentPaths[unreadable] {
		t.Fatal("путь нечитаемого модуля должен остаться в persistentPaths")
	}
}

// TestCacheRefreshRemovesBrokenSymlinkEntry: битый симлинк не листится
// discovery (Resolve его не найдёт), поэтому его запись вычищается из кэша
// как модуль, которого больше нет в source.
func TestCacheRefreshRemovesBrokenSymlinkEntry(t *testing.T) {
	dir := t.TempDir()
	brokenLink := filepath.Join(dir, "broken.yaml")
	if err := os.Symlink(filepath.Join(dir, "nonexistent.yaml"), brokenLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	cache.mu.Lock()
	cache.entries[brokenLink] = entry{
		Module:  &execmodule.Module{Name: "cached_broken"},
		ModTime: time.Now().Add(-1 * time.Hour),
		Size:    100,
	}
	cache.persistentPaths[brokenLink] = true
	cache.mu.Unlock()

	restore := suppressLog(t)
	defer restore()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("Refresh: Total=%d, want 0 (битый симлинк не в source)", result.Total)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.entries[brokenLink]; ok {
		t.Fatal("запись битого симлинка должна быть удалена из entries")
	}
	if cache.persistentPaths[brokenLink] {
		t.Fatal("путь битого симлинка должен быть удалён из persistentPaths")
	}
}

// TestCacheRefreshFIFOInTreeDoesNotHang: FIFO с именем *.yaml в дереве модулей
// не листится, и Refresh не зависает на его чтении (hashFile/LoadModule).
func TestCacheRefreshFIFOInTreeDoesNotHang(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ok.yaml"), "name: ok\n")
	fifo := filepath.Join(dir, "fifo.yaml")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	// Снимаем возможную блокировку, чтобы горутина не протекла при падении теста.
	t.Cleanup(func() {
		if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	type refreshOutcome struct {
		result RefreshResult
		err    error
	}
	done := make(chan refreshOutcome, 1)
	go func() {
		result, err := cache.Refresh(src)
		done <- refreshOutcome{result, err}
	}()
	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("Refresh: %v", out.err)
		}
		if out.result.Total != 1 || out.result.Updated != 1 || out.result.Skipped != 0 {
			t.Fatalf("Refresh: Total=%d Updated=%d Skipped=%d, want 1, 1, 0 (только ok.yaml)",
				out.result.Total, out.result.Updated, out.result.Skipped)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh завис на FIFO в дереве модулей")
	}
}

// manifestLimit — лимит размера манифеста (1 МиБ), общий с execmodule.
const manifestLimit = 1 << 20

// mkfifoUnblocked создаёт FIFO и снимает возможную блокировку его читателя в
// t.Cleanup, чтобы горутина не протекла при падении теста.
func mkfifoUnblocked(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	t.Cleanup(func() {
		if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})
}

// withTimeout выполняет fn в горутине и падает, если она не завершилась за 5 с.
func withTimeout(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: завис", what)
	}
}

// TestHashFileRespectsManifestLimit: hashFile не читает то, что execmodule не
// примет как манифест: FIFO (чтение блокируется) и файл больше лимита
// (отвергается до чтения целиком); файл ровно в лимит хешируется.
func TestHashFileRespectsManifestLimit(t *testing.T) {
	t.Run("fifo", func(t *testing.T) {
		fifo := filepath.Join(t.TempDir(), "x.yaml")
		mkfifoUnblocked(t, fifo)
		var err error
		withTimeout(t, "hashFile(FIFO)", func() { _, err = hashFile(fifo) })
		if err == nil {
			t.Fatal("hashFile(FIFO): ожидалась ошибка")
		}
	})

	t.Run("больше лимита", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "big.yaml")
		writeFile(t, path, strings.Repeat("a", manifestLimit+1))
		_, err := hashFile(path)
		if err == nil || !strings.Contains(err.Error(), "размер") || !strings.Contains(err.Error(), path) {
			t.Fatalf("hashFile(файл > 1 МиБ): err = %v, want ошибка с \"размер\" и путём", err)
		}
	})

	t.Run("ровно лимит", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ok.yaml")
		content := strings.Repeat("a", manifestLimit)
		writeFile(t, path, content)
		got, err := hashFile(path)
		if err != nil {
			t.Fatalf("hashFile(файл ровно в лимит): %v", err)
		}
		want := sha256.Sum256([]byte(content))
		if !bytes.Equal(got, want[:]) {
			t.Fatalf("hashFile = %x, want %x", got, want)
		}
	})
}

// TestLoadModuleVerifiedRejectsFIFO: путь исполнения (`futi x`) не зависает
// на FIFO вместо манифеста — LoadModuleVerified возвращает ошибку.
func TestLoadModuleVerifiedRejectsFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "x.yaml")
	mkfifoUnblocked(t, fifo)

	cache := newTestCache()
	var err error
	withTimeout(t, "LoadModuleVerified(FIFO)", func() { _, err = cache.LoadModuleVerified(fifo, dir) })
	if err == nil {
		t.Fatal("LoadModuleVerified(FIFO): ожидалась ошибка")
	}
}

// TestCacheRefreshSkipsOversizedAndFIFOManifests: манифест больше лимита и
// FIFO (FileSource листит путь без проверки типа) Refresh пропускает с
// причиной hash — не читая файл целиком и не зависая.
func TestCacheRefreshSkipsOversizedAndFIFOManifests(t *testing.T) {
	defer suppressLog(t)()

	t.Run("больше лимита", func(t *testing.T) {
		dir := t.TempDir()
		big := filepath.Join(dir, "big.yaml")
		writeFile(t, big, "name: big\n# "+strings.Repeat("a", manifestLimit))

		result, err := newTestCache().Refresh(discovery.NewGlobalSource(dir))
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if result.Total != 1 || result.Skipped != 1 || len(result.SkippedModules) != 1 {
			t.Fatalf("Refresh: Total=%d Skipped=%d SkippedModules=%+v, want 1, 1, одна запись", result.Total, result.Skipped, result.SkippedModules)
		}
		reason := result.SkippedModules[0].Reason
		if !strings.HasPrefix(reason, "hash:") || !strings.Contains(reason, "размер") {
			t.Fatalf("Reason = %q, want \"hash: …размер…\"", reason)
		}
	})

	t.Run("fifo", func(t *testing.T) {
		dir := t.TempDir()
		fifo := filepath.Join(dir, "x.yaml")
		mkfifoUnblocked(t, fifo)

		var (
			result RefreshResult
			err    error
		)
		withTimeout(t, "Refresh(FIFO)", func() {
			result, err = newTestCache().Refresh(discovery.NewFileSource(fifo, dir, "x"))
		})
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if result.Total != 1 || result.Skipped != 1 || len(result.SkippedModules) != 1 ||
			!strings.HasPrefix(result.SkippedModules[0].Reason, "hash:") {
			t.Fatalf("Refresh: Total=%d Skipped=%d SkippedModules=%+v, want 1, 1, причина hash", result.Total, result.Skipped, result.SkippedModules)
		}
	})
}

// TestCacheRefreshEmptySourceUpdatesLastRefresh проверяет, что пустой source
// (Total=0) обновляет lastRefresh — пустой кэш это валидное состояние, retry не нужен.
func TestCacheRefreshEmptySourceUpdatesLastRefresh(t *testing.T) {
	src := discovery.NewGlobalSource(t.TempDir())
	cache := newTestCache()

	oldRefresh := time.Now().Add(-1 * time.Hour)
	cache.mu.Lock()
	cache.lastRefresh = oldRefresh
	cache.mu.Unlock()

	before := time.Now()
	_, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.lastRefresh.Equal(oldRefresh) || cache.lastRefresh.Before(before) {
		t.Fatalf("expected lastRefresh to be updated for empty source, got %v (old=%v, before=%v)",
			cache.lastRefresh, oldRefresh, before)
	}
}

// --- Group 3a: Cache.Load (extract from Start, fixes #4 double Refresh) ---

// TestCacheLoadPopulatesEntries проверяет, что Load загружает entries из storage
// без вызова Refresh. Используется для `futi cache refresh` — Load + явный Refresh
// вместо Start (который тоже Refresh по политике — двойной Refresh, проблема #4).
func TestCacheLoadPopulatesEntries(t *testing.T) {
	globalPath := "/fake/global.yaml"
	storage := &fakeStorage{
		loadData: cacheFile{
			Version:         cacheFileVersion,
			LastRefreshTime: time.Now().Add(-1 * time.Hour),
			Entries: map[string]entry{
				globalPath: {
					Module:  &execmodule.Module{Name: "cached_global"},
					ModTime: time.Now().Add(-2 * time.Hour),
					Size:    200,
				},
			},
		},
	}
	cache := New(storage, ForceOnlyPolicy{})

	cf := cache.Load()

	if cf.Version != cacheFileVersion {
		t.Fatalf("expected Version=%d, got %d", cacheFileVersion, cf.Version)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	e, ok := cache.entries[globalPath]
	if !ok {
		t.Fatal("expected entry loaded from storage")
	}
	if e.Module == nil || e.Module.Name != "cached_global" {
		t.Fatalf("expected cached_global, got %v", e.Module)
	}
	if !cache.persistentPaths[globalPath] {
		t.Fatal("expected globalPath in persistentPaths")
	}
}

// TestCacheLoadOnCorruptedStorageReturnsEmpty проверяет, что при ошибке Load
// (повреждённый/несовместимый файл) Load возвращает пустую cacheFile и не падает.
func TestCacheLoadOnCorruptedStorageReturnsEmpty(t *testing.T) {
	storage := &fakeStorage{loadErr: errCorruptedForTest}
	cache := New(storage, ForceOnlyPolicy{})

	cf := cache.Load()

	if cf.Version != 0 {
		t.Fatalf("expected Version=0 for corrupted storage, got %d", cf.Version)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) != 0 {
		t.Fatalf("expected empty entries, got %d", len(cache.entries))
	}
	if len(cache.persistentPaths) != 0 {
		t.Fatalf("expected empty persistentPaths, got %d", len(cache.persistentPaths))
	}
}

// TestCacheLoadDoesNotRefresh проверяет, что Load НЕ вызывает Refresh —
// modules из source не загружаются, только из storage.
func TestCacheLoadDoesNotRefresh(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)

	storage := &fakeStorage{}
	cache := New(storage, ForceOnlyPolicy{})

	// Source с модулем, но Load не принимает source — Refresh не вызывается.
	cache.Load()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.entries[modPath]; ok {
		t.Fatal("expected module NOT loaded by Load (no Refresh), but it's in entries")
	}
}

// --- #42: Load() logs storage error ---

// TestCacheLoadLogsStorageError проверяет, что Load() логирует ошибку storage.Load()
// через log.Printf, а не молча возвращает пустую cacheFile. Непоследовательно с
// Save, который логируется.
func TestCacheLoadLogsStorageError(t *testing.T) {
	storage := &fakeStorage{loadErr: errCorruptedForTest}
	cache := New(storage, ForceOnlyPolicy{})

	var buf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOut)

	cf := cache.Load()

	if cf.Version != 0 {
		t.Fatalf("expected Version=0 for corrupted storage, got %d", cf.Version)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, errCorruptedForTest.Error()) {
		t.Fatalf("expected log to mention Load error %q, got: %q", errCorruptedForTest.Error(), logOutput)
	}
	if !strings.Contains(logOutput, "load") {
		t.Fatalf("expected log to mention 'load', got: %q", logOutput)
	}
}

// --- #15: RefreshResult details (updated/skipped module lists) ---

// TestCacheRefreshResultContainsUpdatedModules проверяет, что RefreshResult
// содержит список путей обновлённых модулей (для --verbose).
func TestCacheRefreshResultContainsUpdatedModules(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "app.yaml")
	writeFile(t, modPath, `name: app`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(result.UpdatedModules) != 1 {
		t.Fatalf("expected 1 UpdatedModule, got %d", len(result.UpdatedModules))
	}
	if result.UpdatedModules[0] != modPath {
		t.Fatalf("expected UpdatedModule %q, got %q", modPath, result.UpdatedModules[0])
	}
}

// TestCacheRefreshResultContainsSkippedModules проверяет, что RefreshResult
// содержит список пропущенных модулей с причиной (для --verbose).
func TestCacheRefreshResultContainsSkippedModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "good.yaml"), `name: good`)
	writeFile(t, filepath.Join(dir, "bad.yaml"), `name: bad
commands:
  empty: {}`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()
	restore := suppressLog(t)
	defer restore()

	result, err := cache.Refresh(src)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(result.SkippedModules) != 1 {
		t.Fatalf("expected 1 SkippedModule, got %d", len(result.SkippedModules))
	}
	badPath := filepath.Join(dir, "bad.yaml")
	if result.SkippedModules[0].Path != badPath {
		t.Fatalf("expected SkippedModule Path %q, got %q", badPath, result.SkippedModules[0].Path)
	}
	if result.SkippedModules[0].Reason == "" {
		t.Fatal("expected non-empty Reason for skipped module")
	}
}

// --- #17: Cache introspection methods ---

// TestCacheLastRefresh проверяет, что LastRefresh() возвращает время последнего
// обновления (для futi cache status).
func TestCacheLastRefresh(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), `name: app`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	if !cache.LastRefresh().IsZero() {
		t.Fatal("expected zero LastRefresh before Refresh")
	}

	before := time.Now()
	if _, err := cache.Refresh(src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	lr := cache.LastRefresh()
	if lr.IsZero() || lr.Before(before) {
		t.Fatalf("expected LastRefresh >= %v, got %v", before, lr)
	}
}

// TestCacheModuleCount проверяет, что ModuleCount() возвращает количество
// модулей в in-memory кэше (для futi cache status).
func TestCacheModuleCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), `name: a`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `name: b`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	if cache.ModuleCount() != 0 {
		t.Fatalf("expected 0 modules before Refresh, got %d", cache.ModuleCount())
	}

	if _, err := cache.Refresh(src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if cache.ModuleCount() != 2 {
		t.Fatalf("expected 2 modules after Refresh, got %d", cache.ModuleCount())
	}
}

// TestCachePolicyName проверяет, что PolicyName() возвращает имя политики
// (для futi cache status).
func TestCachePolicyName(t *testing.T) {
	tests := []struct {
		policy RefreshPolicy
		want   string
	}{
		{AlwaysPolicy{}, "every_time"},
		{EveryTMinPolicy{T: 30 * time.Minute}, "every_t_min"},
		{ForceOnlyPolicy{}, "force_only"},
	}
	for _, tc := range tests {
		cache := New(&fakeStorage{}, tc.policy)
		got := cache.PolicyName()
		if got != tc.want {
			t.Fatalf("PolicyName() = %q, want %q", got, tc.want)
		}
	}
}

// --- #11: RWMutex — concurrent LoadModule with Refresh ---

// TestCacheConcurrentLoadModuleAndRefresh проверяет, что параллельные вызовы
// LoadModule и Refresh не вызывают data races и не падают.
// Запускать с -race: go test -race -run TestCacheConcurrentLoadModuleAndRefresh
func TestCacheConcurrentLoadModuleAndRefresh(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "echo_mod.yaml")
	writeFile(t, modPath, `name: echo_mod
cmd: "echo {{.args}}"`)

	src := discovery.NewGlobalSource(dir)
	cache := newTestCache()

	var wg sync.WaitGroup
	const goroutines = 5

	// Половина горутин — LoadModule, половина — Refresh
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if id%2 == 0 {
					_, _ = cache.LoadModule(modPath, dir)
				} else {
					_, _ = cache.Refresh(src)
				}
			}
		}(g)
	}
	wg.Wait()
}

// --- Group 5: Concurrent LoadModule (#29) ---

// TestCacheLoadModuleConcurrent проверяет потокобезопасность LoadModule:
// несколько горутин одновременно загружают тот же манифест. Все должны
// получить один и тот же *Module (cache hit после первой загрузки).
// Запускать с -race для проверки data races: go test -race -run TestCacheLoadModuleConcurrent
func TestCacheLoadModuleConcurrent(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	writeFile(t, manifestPath, `name: echo_mod
cmd: "echo {{.args}}"`)

	cache := newTestCache()

	var wg sync.WaitGroup
	const goroutines = 10
	const iterations = 50
	mods := make([]*execmodule.Module, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				mod, err := cache.LoadModule(manifestPath, dir)
				if err != nil {
					t.Errorf("goroutine %d, iter %d: %v", offset, i, err)
					return
				}
				mods[offset*iterations+i] = mod
			}
		}(g)
	}
	wg.Wait()

	// Все загрузки должны вернуть тот же pointer (cache hit после первой).
	first := mods[0]
	if first == nil {
		t.Fatal("expected non-nil module")
	}
	for i, mod := range mods {
		if mod != first {
			t.Fatalf("load %d: expected same pointer (cache hit), got %p (want %p)", i, mod, first)
		}
	}
}

// --- LoadModuleVerified (строгая проверка свежести/аутентичности) ---

// TestLoadModuleVerifiedReturnsCachedWhenUnchanged проверяет, что при неизменном
// файле (SHA-256 совпал) LoadModuleVerified возвращает кэшированный модуль
// (тот же *Module), не перечитывая YAML с диска.
func TestLoadModuleVerifiedReturnsCachedWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.yaml")
	writeFile(t, manifestPath, "name: m\ncmd: \"echo hi\"\n")

	cache := newTestCache()

	mod1, err := cache.LoadModuleVerified(manifestPath, dir)
	if err != nil {
		t.Fatalf("first verified load: %v", err)
	}
	mod2, err := cache.LoadModuleVerified(manifestPath, dir)
	if err != nil {
		t.Fatalf("second verified load: %v", err)
	}
	if mod1 != mod2 {
		t.Fatalf("expected same *Module (hash match, cache hit), got %p and %p", mod1, mod2)
	}
}

// TestLoadModuleVerifiedReloadsWhenFileChanged проверяет, что при изменении файла
// (SHA-256 не совпал) LoadModuleVerified перечитывает модуль с диска и возвращает
// актуальное содержимое (новый *Module).
func TestLoadModuleVerifiedReloadsWhenFileChanged(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.yaml")
	writeFile(t, manifestPath, "name: m\ncmd: \"echo v1\"\n")

	cache := newTestCache()

	mod1, err := cache.LoadModuleVerified(manifestPath, dir)
	if err != nil {
		t.Fatalf("first verified load: %v", err)
	}
	if mod1.Cmd != "echo v1" {
		t.Fatalf("expected cmd v1, got %q", mod1.Cmd)
	}

	// Меняем содержимое файла.
	writeFile(t, manifestPath, "name: m\ncmd: \"echo v2\"\n")

	mod2, err := cache.LoadModuleVerified(manifestPath, dir)
	if err != nil {
		t.Fatalf("second verified load: %v", err)
	}
	if mod2.Cmd != "echo v2" {
		t.Fatalf("expected reloaded cmd v2, got %q", mod2.Cmd)
	}
	if mod1 == mod2 {
		t.Fatal("expected a different *Module after file change (reload from disk)")
	}
}

// TestLoadModuleVerifiedReloadsWhenHashEmpty проверяет, что запись без Hash
// (старый формат кэша или entry, добавленная через LoadModule) не доверяется:
// LoadModuleVerified перечитывает модуль с диска.
func TestLoadModuleVerifiedReloadsWhenHashEmpty(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.yaml")
	writeFile(t, manifestPath, "name: m\ncmd: \"echo hi\"\n")

	cache := newTestCache()

	// Обычный LoadModule создаёт entry БЕЗ Hash.
	if _, err := cache.LoadModule(manifestPath, dir); err != nil {
		t.Fatalf("plain load: %v", err)
	}

	// Verified должен перечитать (hash пуст), но вернуть валидный модуль.
	mod, err := cache.LoadModuleVerified(manifestPath, dir)
	if err != nil {
		t.Fatalf("verified load after plain: %v", err)
	}
	if mod.Name != "m" {
		t.Fatalf("expected module m, got %q", mod.Name)
	}

	// После verified entry должна иметь Hash.
	cache.mu.RLock()
	e, ok := cache.entries[manifestPath]
	cache.mu.RUnlock()
	if !ok {
		t.Fatal("expected entry to exist")
	}
	if len(e.Hash) == 0 {
		t.Fatal("expected entry to have a Hash after LoadModuleVerified")
	}
}

// TestLoadModuleVerifiedReloadsWhenCacheTampered проверяет защиту от подмены кэша:
// если в entry вставлен чужой Module (и неверный Hash), LoadModuleVerified
// перечитывает модуль с диска и возвращает реальное содержимое, а не подменённое.
func TestLoadModuleVerifiedReloadsWhenCacheTampered(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.yaml")
	writeFile(t, manifestPath, "name: m\ncmd: \"echo legit\"\n")

	cache := newTestCache()

	if _, err := cache.LoadModuleVerified(manifestPath, dir); err != nil {
		t.Fatalf("initial verified load: %v", err)
	}

	// Подмена кэша: вставляем чужой Module с Cmd атакующего и заведомо неверным Hash.
	evil := &execmodule.Module{Name: "m", Cmd: "echo PWNED", Root: dir}
	cache.mu.Lock()
	cache.entries[manifestPath] = entry{Module: evil, Hash: []byte("deadbeef")}
	cache.mu.Unlock()

	mod, err := cache.LoadModuleVerified(manifestPath, dir)
	if err != nil {
		t.Fatalf("verified load after tamper: %v", err)
	}
	if mod.Cmd != "echo legit" {
		t.Fatalf("expected legit cmd from disk, got %q (cache not trusted)", mod.Cmd)
	}
	if mod == evil {
		t.Fatal("returned the tampered module; expected reload from disk")
	}
}

func TestInvalidateClearsMemoryAndDisk(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "m.yaml")
	writeFile(t, manifest, "name: m\ncommands:\n  run:\n    cmd: echo hi\n")

	path := filepath.Join(dir, "cache.gob")
	storage := NewDiskStorage(path)
	cache := New(storage, AlwaysPolicy{})

	// In-memory entry + persistent запись на диске.
	if _, err := cache.LoadModule(manifest, dir); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	if err := storage.Save(cacheFile{
		Version: cacheFileVersion,
		Entries: map[string]entry{manifest: {Size: 10}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := cache.Invalidate(); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got := cache.ModuleCount(); got != 0 {
		t.Fatalf("expected 0 in-memory entries, got %d", got)
	}
	if !cache.LastRefresh().IsZero() {
		t.Fatal("expected zero lastRefresh after Invalidate")
	}
	cf, err := storage.Load()
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if len(cf.Entries) != 0 {
		t.Fatalf("expected empty disk cache, got %d entries", len(cf.Entries))
	}
}

func TestMemoryStorageClearIsNoOp(t *testing.T) {
	if err := (MemoryStorage{}).Clear(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiskStorageClearMissingFile(t *testing.T) {
	s := NewDiskStorage(filepath.Join(t.TempDir(), "missing.gob"))
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear on missing file must not fail: %v", err)
	}
}

// TestCacheFileOfPreviousVersionDoesNotBypassValidate (audit F75): персистентный
// кэш версии 1 хранил готовые модули без повторной Validate; после ужесточения
// правил (зарезервированные имена параметров) такой кэш должен отбрасываться,
// иначе LoadModuleVerified отдаёт невалидный модуль при совпавшем хэше.
func TestCacheFileOfPreviousVersionDoesNotBypassValidate(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "m.yaml")
	writeFile(t, manifest, "name: m\ncmd: \"echo env={{.env}}\"\nparams:\n  - name: env\n")
	h, err := hashFile(manifest)
	if err != nil {
		t.Fatalf("hashFile: %v", err)
	}

	path := filepath.Join(dir, "cache.gob")
	storage := NewDiskStorage(path)
	if err := storage.Save(cacheFile{
		Version: 1, // версия до ужесточения Validate (F75)
		Entries: map[string]entry{manifest: {
			Module: &execmodule.Module{Name: "m", Cmd: "echo env={{.env}}"},
			Hash:   h,
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cache := New(storage, ForceOnlyPolicy{})
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	cache.Load()

	if _, err := cache.LoadModuleVerified(manifest, dir); err == nil {
		t.Fatal("LoadModuleVerified: module with reserved param name loaded from old-version cache without error")
	}
}
