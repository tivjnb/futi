package discovery

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestResolveSingleYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker.yaml"), "name: docker\n")

	s := NewGlobalSource(dir)
	manifest, root, err := s.Resolve("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(dir, "docker.yaml") {
		t.Fatalf("unexpected manifest path: %q", manifest)
	}
	if root != dir {
		t.Fatalf("unexpected module root: %q, want %q", root, dir)
	}
}

func TestResolvePackage(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "docker")
	mkdir(t, pkgDir)
	writeFile(t, filepath.Join(pkgDir, "config.yaml"), "name: docker\n")

	s := NewGlobalSource(dir)
	manifest, root, err := s.Resolve("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(pkgDir, "config.yaml") {
		t.Fatalf("unexpected manifest path: %q", manifest)
	}
	if root != pkgDir {
		t.Fatalf("unexpected module root: %q, want %q", root, pkgDir)
	}
}

func TestResolveConflictReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker.yaml"), "name: docker\n")
	pkgDir := filepath.Join(dir, "docker")
	mkdir(t, pkgDir)
	writeFile(t, filepath.Join(pkgDir, "config.yaml"), "name: docker\n")

	s := NewGlobalSource(dir)
	_, _, err := s.Resolve("docker")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
}

func TestResolveNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	s := NewGlobalSource(dir)
	_, _, err := s.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected not found error, got nil")
	}
}

func TestResolveNestedNamespace(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "dev")
	mkdir(t, subDir)
	writeFile(t, filepath.Join(subDir, "deploy.yaml"), "name: dev.deploy\n")

	s := NewGlobalSource(dir)
	manifest, root, err := s.Resolve("dev.deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(subDir, "deploy.yaml") {
		t.Fatalf("unexpected manifest path: %q", manifest)
	}
	if root != subDir {
		t.Fatalf("unexpected module root: %q, want %q", root, subDir)
	}
}

func TestResolveNestedNamespacePackage(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev", "deploy")
	mkdir(t, pkgDir)
	writeFile(t, filepath.Join(pkgDir, "config.yaml"), "name: dev.deploy\n")

	s := NewGlobalSource(dir)
	manifest, root, err := s.Resolve("dev.deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(pkgDir, "config.yaml") {
		t.Fatalf("unexpected manifest path: %q", manifest)
	}
	if root != pkgDir {
		t.Fatalf("unexpected module root: %q, want %q", root, pkgDir)
	}
}

func TestListReturnsAllModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir, "git.yaml"), "name: git\n")
	pkgDir := filepath.Join(dir, "k8s")
	mkdir(t, pkgDir)
	writeFile(t, filepath.Join(pkgDir, "config.yaml"), "name: k8s\n")

	s := NewGlobalSource(dir)
	modules, err := s.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(modules))
	}

	names := make(map[string]bool)
	for _, m := range modules {
		names[m.Name] = true
	}
	for _, expected := range []string{"docker", "git", "k8s"} {
		if !names[expected] {
			t.Fatalf("missing module %q in list", expected)
		}
	}
}

func TestListNonExistentDirReturnsEmpty(t *testing.T) {
	s := NewGlobalSource("/nonexistent/path/that/does/not/exist")
	modules, err := s.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if modules != nil && len(modules) != 0 {
		t.Fatalf("expected nil or empty, got %d modules", len(modules))
	}
}

func TestListChildrenRootReturnsModulesAndNamespaces(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker.yaml"), "name: docker\ndescription: Docker management\n")
	writeFile(t, filepath.Join(dir, "git.yaml"), "name: git\n")
	nsDir := filepath.Join(dir, "images")
	mkdir(t, nsDir)
	writeFile(t, filepath.Join(nsDir, "png.yaml"), "name: images.png\n")

	s := NewGlobalSource(dir)
	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}

	byName := make(map[string]Child)
	for _, c := range children {
		byName[c.Name] = c
	}

	if c, ok := byName["docker"]; !ok || c.Type != "module" {
		t.Fatalf("expected module 'docker', got %+v", c)
	}
	if c, ok := byName["git"]; !ok || c.Type != "module" {
		t.Fatalf("expected module 'git', got %+v", c)
	}
	if c, ok := byName["images"]; !ok || c.Type != "namespace" {
		t.Fatalf("expected namespace 'images', got %+v", c)
	}
}

func TestListChildrenNamespaceReturnsNested(t *testing.T) {
	dir := t.TempDir()
	nsDir := filepath.Join(dir, "images")
	mkdir(t, nsDir)
	writeFile(t, filepath.Join(nsDir, "png.yaml"), "name: images.png\ndescription: PNG utilities\n")
	converterDir := filepath.Join(nsDir, "converter")
	mkdir(t, converterDir)
	writeFile(t, filepath.Join(converterDir, "config.yaml"), "name: images.converter\n")

	s := NewGlobalSource(dir)
	children, err := s.ListChildren("images")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}

	byName := make(map[string]Child)
	for _, c := range children {
		byName[c.Name] = c
	}

	if c, ok := byName["png"]; !ok || c.Type != "module" {
		t.Fatalf("expected module 'png', got %+v", c)
	}
	if c, ok := byName["converter"]; !ok || c.Type != "module" {
		t.Fatalf("expected module 'converter' (package), got %+v", c)
	}
}

func TestListChildrenNonExistentReturnsError(t *testing.T) {
	dir := t.TempDir()
	s := NewGlobalSource(dir)
	_, err := s.ListChildren("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent namespace")
	}
}

func TestListChildrenEmptyNamespaceReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewGlobalSource(dir)
	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestListChildrenExcludesConfigYamlAtCurrentLevel(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "docker")
	mkdir(t, pkgDir)
	writeFile(t, filepath.Join(pkgDir, "config.yaml"), "name: docker\n")

	s := NewGlobalSource(dir)
	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child (docker package), got %d", len(children))
	}
	if children[0].Name != "docker" || children[0].Type != "module" {
		t.Fatalf("expected module 'docker', got %+v", children[0])
	}
}

func TestListChildrenRecursiveRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker.yaml"), "name: docker\n")
	nsDir := filepath.Join(dir, "images")
	mkdir(t, nsDir)
	writeFile(t, filepath.Join(nsDir, "png.yaml"), "name: images.png\n")
	converterDir := filepath.Join(nsDir, "converter")
	mkdir(t, converterDir)
	writeFile(t, filepath.Join(converterDir, "to_png.yaml"), "name: images.converter.to_png\n")

	s := NewGlobalSource(dir)
	children, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 5 {
		t.Fatalf("expected 5 children, got %d: %+v", len(children), children)
	}

	byName := make(map[string]string)
	for _, c := range children {
		byName[c.Name] = c.Type
	}
	if byName["docker"] != "module" {
		t.Fatalf("expected module 'docker', got %q", byName["docker"])
	}
	if byName["images"] != "namespace" {
		t.Fatalf("expected namespace 'images', got %q", byName["images"])
	}
	if byName["images.png"] != "module" {
		t.Fatalf("expected module 'images.png', got %q", byName["images.png"])
	}
	if byName["images.converter"] != "namespace" {
		t.Fatalf("expected namespace 'images.converter', got %q", byName["images.converter"])
	}
	if byName["images.converter.to_png"] != "module" {
		t.Fatalf("expected module 'images.converter.to_png', got %q", byName["images.converter.to_png"])
	}
}

func TestListChildrenRecursiveFromNamespace(t *testing.T) {
	dir := t.TempDir()
	nsDir := filepath.Join(dir, "images")
	mkdir(t, nsDir)
	writeFile(t, filepath.Join(nsDir, "png.yaml"), "name: images.png\n")
	converterDir := filepath.Join(nsDir, "converter")
	mkdir(t, converterDir)
	writeFile(t, filepath.Join(converterDir, "to_png.yaml"), "name: images.converter.to_png\n")

	s := NewGlobalSource(dir)
	children, err := s.ListChildrenRecursive("images")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d: %+v", len(children), children)
	}

	byName := make(map[string]string)
	for _, c := range children {
		byName[c.Name] = c.Type
	}
	if byName["png"] != "module" {
		t.Fatalf("expected module 'png', got %q", byName["png"])
	}
	if byName["converter"] != "namespace" {
		t.Fatalf("expected namespace 'converter', got %q", byName["converter"])
	}
	if byName["converter.to_png"] != "module" {
		t.Fatalf("expected module 'converter.to_png', got %q", byName["converter.to_png"])
	}
}

func TestListChildrenRecursiveNonExistentReturnsError(t *testing.T) {
	dir := t.TempDir()
	s := NewGlobalSource(dir)
	_, err := s.ListChildrenRecursive("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent namespace")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

// --- Поддержка симлинков (dotfiles-менеджеры, dev-симлинки) ---

// symlinkOrFail — хелпер: создаёт симлинк или пропускает тест
// (платформа без поддержки симлинков).
func symlinkOrFail(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
}

// mustListNames собирает имена модулей из List() в set.
func listNames(t *testing.T, s *GlobalSource) map[string]string {
	t.Helper()
	modules, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make(map[string]string, len(modules))
	for _, m := range modules {
		names[m.Name] = m.ManifestPath
	}
	return names
}

// TestListSymlinkedModules проверяет, что симлинки обрабатываются так же,
// как реальные файлы/директории: симлинк на .yaml-файл, симлинк на пакет
// (директорию с config.yaml) и симлинк на namespace видны в List,
// ListChildren и ListChildrenRecursive. До фикса симлинк-пакет был
// «призрачным»: исполнялся через Resolve, но не листился.
func TestListSymlinkedModules(t *testing.T) {
	dir := t.TempDir()

	// Реальное содержимое вне RootDir (эмулирует dotfiles-репо).
	outside := filepath.Join(dir, "outside")
	mkdir(t, outside)
	writeFile(t, filepath.Join(outside, "linked.yaml"), "name: linked\n")
	pkgReal := filepath.Join(outside, "realpkg")
	mkdir(t, pkgReal)
	writeFile(t, filepath.Join(pkgReal, "config.yaml"), "name: realpkg\n")
	nsReal := filepath.Join(outside, "realns")
	mkdir(t, nsReal)
	writeFile(t, filepath.Join(nsReal, "inner.yaml"), "name: realns.inner\n")

	// RootDir с симлинками всех трёх видов.
	root := filepath.Join(dir, "root")
	mkdir(t, root)
	symlinkOrFail(t, filepath.Join(outside, "linked.yaml"), filepath.Join(root, "linked.yaml"))
	symlinkOrFail(t, pkgReal, filepath.Join(root, "linkedpkg"))
	symlinkOrFail(t, nsReal, filepath.Join(root, "linkedns"))

	s := NewGlobalSource(root)

	// List: все три вида видны как модули.
	names := listNames(t, s)
	for _, expected := range []string{"linked", "linkedpkg", "linkedns.inner"} {
		if _, ok := names[expected]; !ok {
			t.Fatalf("List: missing module %q (ghost module), got %v", expected, names)
		}
	}

	// ListChildren: типы определены корректно.
	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	byName := make(map[string]Child)
	for _, c := range children {
		byName[c.Name] = c
	}
	if c, ok := byName["linked"]; !ok || c.Type != "module" {
		t.Fatalf("ListChildren: expected module 'linked', got %v", byName)
	}
	if c, ok := byName["linkedpkg"]; !ok || c.Type != "module" {
		t.Fatalf("ListChildren: expected module 'linkedpkg', got %v", byName)
	}
	if c, ok := byName["linkedns"]; !ok || c.Type != "namespace" {
		t.Fatalf("ListChildren: expected namespace 'linkedns', got %v", byName)
	}

	// ListChildrenRecursive: модуль внутри симлинк-namespace тоже виден.
	rec, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("ListChildrenRecursive: %v", err)
	}
	recNames := make(map[string]bool)
	for _, c := range rec {
		recNames[c.Name] = true
	}
	for _, expected := range []string{"linked", "linkedpkg", "linkedns", "linkedns.inner"} {
		if !recNames[expected] {
			t.Fatalf("ListChildrenRecursive: missing %q, got %v", expected, recNames)
		}
	}
}

// TestListSymlinkCycleTerminates проверяет защиту от циклов симлинков:
// взаимные симлинки a → b → a не приводят к бесконечной рекурсии.
// Семантика: каталог, уже лежащий среди предков текущего пути, не
// раскрывается повторно (a.loop.loop не существует), но содержимое
// доступно и прямым путём, и через алиас. Дубликатов по ManifestPath нет.
func TestListSymlinkCycleTerminates(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	mkdir(t, root)
	a := filepath.Join(root, "a")
	mkdir(t, a)
	writeFile(t, filepath.Join(a, "mod.yaml"), "name: a.mod\n")
	b := filepath.Join(root, "b")
	mkdir(t, b)
	writeFile(t, filepath.Join(b, "other.yaml"), "name: b.other\n")

	// Цикл: a/loop → b, b/loop → a.
	symlinkOrFail(t, b, filepath.Join(a, "loop"))
	symlinkOrFail(t, a, filepath.Join(b, "loop"))

	s := NewGlobalSource(root)

	// List завершается, дубликатов нет, оба реальных модуля достижимы.
	modules, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	seen := make(map[string]bool)
	for _, m := range modules {
		if seen[m.ManifestPath] {
			t.Fatalf("duplicate ManifestPath %q in %v", m.ManifestPath, modules)
		}
		seen[m.ManifestPath] = true
	}
	if !seen[filepath.Join(a, "mod.yaml")] {
		t.Fatalf("expected a/mod.yaml, got %v", modules)
	}
	// b/other.yaml достижим через первый встреченный путь (a/loop).
	foundOther := false
	for _, m := range modules {
		if filepath.Base(m.ManifestPath) == "other.yaml" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Fatalf("expected other.yaml (via a/loop), got %v", modules)
	}

	// ListChildrenRecursive тоже завершается; дубликатов module-записей нет
	// (namespace-записи имеют пустой ManifestPath by design).
	rec, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("ListChildrenRecursive: %v", err)
	}
	recSeen := make(map[string]bool)
	for _, c := range rec {
		if c.Type != "module" {
			continue
		}
		if recSeen[c.ManifestPath] {
			t.Fatalf("recursive: duplicate ManifestPath %q in %v", c.ManifestPath, rec)
		}
		recSeen[c.ManifestPath] = true
	}
}

// TestListSymlinkPackageAliasesBothListed: два симлинка-алиаса на один
// реальный ПАКЕТ — оба листятся (консистентно с Resolve, который находит
// модуль по обоим именам). Дедупликация по реальному пути применяется
// только к рекурсивному обходу namespace, пакеты не рекурсируются.
func TestListSymlinkPackageAliasesBothListed(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	mkdir(t, root)
	pkgReal := filepath.Join(dir, "realpkg")
	mkdir(t, pkgReal)
	writeFile(t, filepath.Join(pkgReal, "config.yaml"), "name: realpkg\n")

	symlinkOrFail(t, pkgReal, filepath.Join(root, "aliaspkg"))
	symlinkOrFail(t, pkgReal, filepath.Join(root, "linkedpkg"))

	s := NewGlobalSource(root)
	names := listNames(t, s)
	if len(names) != 2 {
		t.Fatalf("expected both package aliases listed, got %v", names)
	}
	if _, ok := names["aliaspkg"]; !ok {
		t.Fatalf("missing alias 'aliaspkg', got %v", names)
	}
	if _, ok := names["linkedpkg"]; !ok {
		t.Fatalf("missing alias 'linkedpkg', got %v", names)
	}
}

// TestListSkipsBrokenSymlink: битый симлинк на .yaml не листится — Resolve
// его не найдёт (ErrNotFound), а листинг показывает только резолвимое.
func TestListSkipsBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real.yaml"), "name: real\n")
	symlinkOrFail(t, filepath.Join(dir, "nonexistent.yaml"), filepath.Join(dir, "broken.yaml"))

	s := NewGlobalSource(dir)
	if _, _, err := s.Resolve("broken"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve(\"broken\") err = %v, want ErrNotFound", err)
	}
	names := listNames(t, s)
	if len(names) != 1 {
		t.Fatalf("List = %v, want только real", names)
	}
	if _, ok := names["real"]; !ok {
		t.Fatalf("List = %v, want только real", names)
	}
}

// writeUnresolvableFixture создаёт в root набор записей, которые Resolve
// не найдёт (имя с точкой, пустое имя, битый симлинк, FIFO, каталоги config/
// и .git/), плюс валидные ok.yaml и ns/m.yaml.
func writeUnresolvableFixture(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "ok.yaml"), "name: ok\n")
	writeFile(t, filepath.Join(root, "ns", "m.yaml"), "name: m\n")

	writeFile(t, filepath.Join(root, "my.tool.yaml"), "name: tool\n")
	writeFile(t, filepath.Join(root, ".yaml"), "name: empty\n")
	writeFile(t, filepath.Join(root, "foo..yaml"), "name: foo\n")
	writeFile(t, filepath.Join(root, "my.dir", "a.yaml"), "name: a\n")
	writeFile(t, filepath.Join(root, "config", "c.yaml"), "name: c\n")
	writeFile(t, filepath.Join(root, ".git", "hooks.yaml"), "name: hooks\n")
	symlinkOrFail(t, "/nonexistent", filepath.Join(root, "broken.yaml"))
	if err := syscall.Mkfifo(filepath.Join(root, "fifo.yaml"), 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
}

// childKeys возвращает отсортированные "имя:тип" детей.
func childKeys(children []Child) []string {
	keys := make([]string, 0, len(children))
	for _, c := range children {
		keys = append(keys, c.Name+":"+c.Type)
	}
	sort.Strings(keys)
	return keys
}

// TestListSkipsUnresolvableEntries: все три листинга GlobalSource пропускают
// записи, которые Resolve не найдёт: `my.tool.yaml` листился бы как my.tool,
// но Resolve ищет my/tool.yaml; `.yaml` — пустое имя; битый симлинк и FIFO —
// не обычный файл; каталоги с точкой и config/ — сегменты, которые Resolve
// не примет.
func TestListSkipsUnresolvableEntries(t *testing.T) {
	root := t.TempDir()
	writeUnresolvableFixture(t, root)
	s := NewGlobalSource(root)

	modules, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var listed []string
	for _, m := range modules {
		listed = append(listed, m.Name)
	}
	sort.Strings(listed)
	if want := []string{"ns.m", "ok"}; !slices.Equal(listed, want) {
		t.Errorf("List = %v, want %v", listed, want)
	}

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if got, want := childKeys(children), []string{"ns:namespace", "ok:module"}; !slices.Equal(got, want) {
		t.Errorf("ListChildren(\"\") = %v, want %v", got, want)
	}

	rec, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("ListChildrenRecursive: %v", err)
	}
	if got, want := childKeys(rec), []string{"ns.m:module", "ns:namespace", "ok:module"}; !slices.Equal(got, want) {
		t.Errorf("ListChildrenRecursive(\"\") = %v, want %v", got, want)
	}
}

// TestListChildrenRejectsUnresolvableNamespace: аргумент namespace проверяется
// по тем же правилам, что в Resolve. Сегмент "config" зарезервирован →
// «namespace not found»; пустой сегмент ("a..b", ".git") → «invalid namespace:
// empty segment» (filepath.Join схлопнул бы его и залистил root/a/b, root/git).
func TestListChildrenRejectsUnresolvableNamespace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config", "c.yaml"), "name: c\n")
	writeFile(t, filepath.Join(root, "a", "b", "x.yaml"), "name: x\n")
	writeFile(t, filepath.Join(root, "git", "g.yaml"), "name: g\n")
	s := NewGlobalSource(root)

	listings := map[string]func(string) ([]Child, error){
		"ListChildren":          s.ListChildren,
		"ListChildrenRecursive": s.ListChildrenRecursive,
	}
	tests := []struct {
		namespace string
		wantErr   string
	}{
		{"config", "not found"},
		{"a.config", "not found"},
		{"a..b", "empty segment"},
		{".git", "empty segment"},
		{"a.", "empty segment"},
	}
	for method, list := range listings {
		for _, tc := range tests {
			children, err := list(tc.namespace)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s(%q) = %v, %v; want ошибку с %q", method, tc.namespace, childKeys(children), err, tc.wantErr)
			}
		}
	}
}

// TestListingsAgree: List, ListChildrenRecursive и рекурсивный обход через
// ListChildren дают одно и то же множество модулей с теми же манифестами,
// и каждый листнутый модуль находится Resolve по своему имени.
func TestListingsAgree(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	writeUnresolvableFixture(t, root)
	writeFile(t, filepath.Join(root, "dev", "deploy", "config.yaml"), "name: deploy\n")
	writeFile(t, filepath.Join(root, "dev", "deploy", "inner.yaml"), "name: inner\n")
	writeFile(t, filepath.Join(root, "dev", "tools", "lint.yaml"), "name: lint\n")
	writeFile(t, filepath.Join(root, "dev", "tools", "bad.name.yaml"), "name: bad\n")
	writeFile(t, filepath.Join(dir, "outside", "ext.yaml"), "name: ext\n")
	symlinkOrFail(t, filepath.Join(dir, "outside"), filepath.Join(root, "linked"))
	// Конфликт x.yaml vs x/config.yaml: в листингах одна запись cx, Resolve — ошибка.
	writeFile(t, filepath.Join(root, "cx.yaml"), "name: cx\n")
	writeFile(t, filepath.Join(root, "cx", "config.yaml"), "name: cx\n")

	s := NewGlobalSource(root)
	want := map[string]string{
		"cx":             filepath.Join(root, "cx.yaml"),
		"ok":             filepath.Join(root, "ok.yaml"),
		"ns.m":           filepath.Join(root, "ns", "m.yaml"),
		"dev.deploy":     filepath.Join(root, "dev", "deploy", "config.yaml"),
		"dev.tools.lint": filepath.Join(root, "dev", "tools", "lint.yaml"),
		"linked.ext":     filepath.Join(root, "linked", "ext.yaml"),
	}

	fromList := listNames(t, s)

	rec, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("ListChildrenRecursive: %v", err)
	}
	fromRecursive := make(map[string]string)
	for _, c := range rec {
		if c.Type == "module" {
			fromRecursive[c.Name] = c.ManifestPath
		}
	}

	fromChildren := make(map[string]string)
	var walk func(ns string)
	walk = func(ns string) {
		children, err := s.ListChildren(ns)
		if err != nil {
			t.Fatalf("ListChildren(%q): %v", ns, err)
		}
		for _, c := range children {
			full := c.Name
			if ns != "" {
				full = ns + "." + c.Name
			}
			if c.Type == "namespace" {
				walk(full)
				continue
			}
			fromChildren[full] = c.ManifestPath
		}
	}
	walk("")

	for name, got := range map[string]map[string]string{
		"List":                  fromList,
		"ListChildrenRecursive": fromRecursive,
		"обход ListChildren":    fromChildren,
	} {
		if !maps.Equal(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	// map прячет дубли: сверяем и число записей-модулей.
	modules, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(modules) != len(want) {
		t.Errorf("List: %d записей, want %d: %v", len(modules), len(want), modules)
	}
	recModules := 0
	for _, c := range rec {
		if c.Type == "module" {
			recModules++
		}
	}
	if recModules != len(want) {
		t.Errorf("ListChildrenRecursive: %d модулей, want %d", recModules, len(want))
	}

	for name, manifest := range fromList {
		if name == "cx" {
			continue // конфликт: Resolve возвращает ошибку по замыслу
		}
		got, _, err := s.Resolve(name)
		if err != nil || got != manifest {
			t.Errorf("Resolve(%q) = %q, %v; want %q (листинг показал)", name, got, err, manifest)
		}
	}
}

// TestListConflictYamlVsPackageIsMarked: x.yaml рядом с x/config.yaml в
// листингах — одна запись x с Conflict (манифест от x.yaml), а Resolve
// по-прежнему возвращает ошибку конфликта, не ErrNotFound.
func TestListConflictYamlVsPackageIsMarked(t *testing.T) {
	for _, ns := range []string{"", "ns"} {
		t.Run("namespace="+ns, func(t *testing.T) {
			root := t.TempDir()
			base := root
			if ns != "" {
				base = filepath.Join(root, ns)
			}
			writeFile(t, filepath.Join(base, "x.yaml"), "name: x\n")
			writeFile(t, filepath.Join(base, "x", "config.yaml"), "name: x\n")
			writeFile(t, filepath.Join(base, "y.yaml"), "name: y\n")
			s := NewGlobalSource(root)
			wantManifest := filepath.Join(base, "x.yaml")

			check := func(label string, children []Child, err error, wantName string) {
				t.Helper()
				if err != nil {
					t.Fatalf("%s: %v", label, err)
				}
				var got []Child
				for _, c := range children {
					if c.Name == wantName {
						got = append(got, c)
					}
				}
				if len(got) != 1 {
					t.Fatalf("%s: записей %q = %d (%v), want 1", label, wantName, len(got), children)
				}
				if got[0].Type != "module" || !got[0].Conflict || got[0].ManifestPath != wantManifest || got[0].ModuleRoot != base {
					t.Errorf("%s: %+v, want module Conflict=true ManifestPath=%q ModuleRoot=%q", label, got[0], wantManifest, base)
				}
				for _, c := range children {
					if c.Name != wantName && c.Conflict {
						t.Errorf("%s: Conflict у неконфликтной записи %+v", label, c)
					}
				}
			}

			children, err := s.ListChildren(ns)
			check("ListChildren", children, err, "x")

			rec, err := s.ListChildrenRecursive(ns)
			check("ListChildrenRecursive", rec, err, "x")

			fullName := "x"
			if ns != "" {
				fullName = ns + ".x"
			}
			modules, err := s.List()
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			count := 0
			for _, m := range modules {
				if m.Name == fullName {
					count++
					if m.ManifestPath != wantManifest {
						t.Errorf("List: ManifestPath = %q, want %q", m.ManifestPath, wantManifest)
					}
				}
			}
			if count != 1 {
				t.Errorf("List: записей %q = %d, want 1", fullName, count)
			}

			_, _, err = s.Resolve(fullName)
			if err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("Resolve(%q) err = %v, want ошибка конфликта (не ErrNotFound)", fullName, err)
			}
		})
	}
}

func TestGlobalSourceResolveEmptyNamespace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), "cmd: echo root\n")
	writeFile(t, filepath.Join(dir, "a.yaml"), "cmd: echo a\n")
	s := NewGlobalSource(dir)

	for _, ns := range []string{"", "a..b", ".a", "a."} {
		if path, _, err := s.Resolve(ns); err == nil {
			t.Errorf("Resolve(%q) = %q, ожидалась ошибка", ns, path)
		}
	}

	path, _, err := s.Resolve("a")
	if err != nil {
		t.Fatalf("Resolve(\"a\"): %v", err)
	}
	if want := filepath.Join(dir, "a.yaml"); path != want {
		t.Errorf("Resolve(\"a\") = %q, want %q", path, want)
	}
}

func TestResolveInsidePackageNotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev", "deploy", "config.yaml"), "name: deploy\n")
	writeFile(t, filepath.Join(dir, "dev", "deploy", "x.yaml"), "name: x\n")

	s := NewGlobalSource(dir)
	_, _, err := s.Resolve("dev.deploy.x")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Resolve(dev.deploy.x) err = %v, want not found", err)
	}
	if _, _, err := s.Resolve("dev.deploy"); err != nil {
		t.Fatalf("Resolve(dev.deploy): unexpected error: %v", err)
	}
}

func TestResolveConfigSegmentRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev", "config.yaml"), "name: dev\n")

	s := NewGlobalSource(dir)
	for _, ns := range []string{"dev.config", "config"} {
		_, _, err := s.Resolve(ns)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("Resolve(%q) err = %v, want not found", ns, err)
		}
	}
}

func TestListDoesNotEnterPackages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev", "deploy", "config.yaml"), "name: deploy\n")
	writeFile(t, filepath.Join(dir, "dev", "deploy", "x.yaml"), "name: x\n")

	s := NewGlobalSource(dir)
	mods, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range mods {
		names = append(names, m.Name)
	}
	if len(names) != 1 || names[0] != "dev.deploy" {
		t.Fatalf("List names = %v, want [dev.deploy]", names)
	}

	children, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatal(err)
	}
	var recMods []string
	for _, c := range children {
		if c.Type == "module" {
			recMods = append(recMods, c.Name)
		}
	}
	if len(recMods) != 1 || recMods[0] != "dev.deploy" {
		t.Fatalf("ListChildrenRecursive modules = %v, want [dev.deploy]", recMods)
	}
}

// Пакет — лист: внутри namespace-пакета (и глубже) детей нет, как и для
// Resolve("pk.inner") → not found.
func TestListChildrenOfPackageIsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pk", "config.yaml"), "name: pk\n")
	writeFile(t, filepath.Join(dir, "pk", "inner.yaml"), "name: inner\n")
	writeFile(t, filepath.Join(dir, "pk", "sub", "x.yaml"), "name: x\n")

	s := NewGlobalSource(dir)
	for _, ns := range []string{"pk", "pk.sub"} {
		children, err := s.ListChildren(ns)
		if err != nil || len(children) != 0 {
			t.Errorf("ListChildren(%q) = %v, %v; want [], nil", ns, children, err)
		}
		children, err = s.ListChildrenRecursive(ns)
		if err != nil || len(children) != 0 {
			t.Errorf("ListChildrenRecursive(%q) = %v, %v; want [], nil", ns, children, err)
		}
	}
}

func TestListPackageSymlinkNotEntered(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	writeFile(t, filepath.Join(real, "config.yaml"), "name: pkg\n")
	writeFile(t, filepath.Join(real, "x.yaml"), "name: x\n")
	root := filepath.Join(dir, "root")
	mkdir(t, root)
	symlinkOrFail(t, real, filepath.Join(root, "pkg"))

	mods, err := NewGlobalSource(root).List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range mods {
		names = append(names, m.Name)
	}
	if len(names) != 1 || names[0] != "pkg" {
		t.Fatalf("List names = %v, want [pkg]", names)
	}
}

func TestGlobalSourceResolveConflictIsNotNotFound(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "a", "b"))
	writeFile(t, filepath.Join(dir, "a", "b.yaml"), "name: b\n")
	writeFile(t, filepath.Join(dir, "a", "b", "config.yaml"), "name: b\n")

	s := NewGlobalSource(dir)

	_, _, err := s.Resolve("a.b")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("conflict must not be ErrNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error %q does not mention conflict", err)
	}

	_, _, err = s.Resolve("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing module: err = %v, want ErrNotFound", err)
	}
	if err.Error() != `discovery: module "missing" not found` {
		t.Fatalf("unexpected message: %q", err)
	}
}

// noAccessDir создаёт каталог без прав доступа; права возвращаются в Cleanup.
func noAccessDir(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root обходит права доступа")
	}
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 не ограничивает доступ в Windows")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

func TestGlobalSourceResolveStatErrorIsNotNotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), "name: a\n")
	mkdir(t, filepath.Join(dir, "a"))
	writeFile(t, filepath.Join(dir, "a", "b.yaml"), "name: b\n")
	noAccessDir(t, filepath.Join(dir, "a"))

	_, _, err := NewGlobalSource(dir).Resolve("a.b")
	if err == nil {
		t.Fatal("ожидалась ошибка stat")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("ошибка stat не должна быть ErrNotFound: %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want fs.ErrPermission", err)
	}
}

func TestGlobalSourceResolveNotDirIsNotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "не каталог\n")

	_, _, err := NewGlobalSource(dir).Resolve("a.b")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err.Error() != `discovery: module "a.b" not found` {
		t.Fatalf("неожиданный текст ошибки: %q", err)
	}
}

func TestGlobalSourceResolvePackageCheckStatError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), "name: a\n")
	mkdir(t, filepath.Join(dir, "a"))
	noAccessDir(t, filepath.Join(dir, "a"))

	_, _, err := NewGlobalSource(dir).Resolve("a")
	if err == nil {
		t.Fatal("ожидалась ошибка stat a/config.yaml")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("ошибка stat не должна быть ErrNotFound: %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("ожидался fs.ErrPermission, получено: %v", err)
	}
}

func TestResolveFIFOManifestIsNotFound(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "x.yaml")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	// Снимаем возможную блокировку, чтобы горутина не протекла при падении теста.
	t.Cleanup(func() {
		if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})

	errCh := make(chan error, 1)
	go func() {
		_, _, err := NewGlobalSource(dir).Resolve("x")
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Resolve(\"x\") с FIFO вместо манифеста: err = %v, want ErrNotFound", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve завис на FIFO")
	}
}

// recursiveEntries — «тип:имя» всех записей ListChildrenRecursive(ns), отсортированные.
func recursiveEntries(t *testing.T, s *GlobalSource, ns string) []string {
	t.Helper()
	rec, err := s.ListChildrenRecursive(ns)
	if err != nil {
		t.Fatalf("ListChildrenRecursive(%q): %v", ns, err)
	}
	var got []string
	for _, c := range rec {
		got = append(got, c.Type+":"+c.Name)
	}
	sort.Strings(got)
	return got
}

// TestRecursiveAliasesAreBothExpanded: два симлинка на один каталог
// (a/x -> real, b/y -> real) раскрываются оба, а не только первый по
// порядку обхода; все листинги согласованы с ListChildren по каждому алиасу.
func TestRecursiveAliasesAreBothExpanded(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	real := filepath.Join(root, "real")
	writeFile(t, filepath.Join(real, "m.yaml"), "name: m\n")
	mkdir(t, filepath.Join(root, "a"))
	mkdir(t, filepath.Join(root, "b"))
	symlinkOrFail(t, real, filepath.Join(root, "a", "x"))
	symlinkOrFail(t, real, filepath.Join(root, "b", "y"))
	s := NewGlobalSource(root)

	wantRec := []string{
		"module:a.x.m", "module:b.y.m", "module:real.m",
		"namespace:a", "namespace:a.x", "namespace:b", "namespace:b.y", "namespace:real",
	}
	if got := recursiveEntries(t, s, ""); !slices.Equal(got, wantRec) {
		t.Errorf("ListChildrenRecursive(\"\") = %v, want %v", got, wantRec)
	}

	wantList := []string{"a.x.m", "b.y.m", "real.m"}
	var gotList []string
	for name := range listNames(t, s) {
		gotList = append(gotList, name)
	}
	sort.Strings(gotList)
	if !slices.Equal(gotList, wantList) {
		t.Errorf("List = %v, want %v", gotList, wantList)
	}

	for _, ns := range []string{"a.x", "b.y"} {
		children, err := s.ListChildren(ns)
		if err != nil || len(children) != 1 || children[0].Name != "m" {
			t.Errorf("ListChildren(%q) = %v, %v; want [m]", ns, children, err)
		}
	}
}

// TestRecursiveLoopStillTerminates: a/loop -> .. (в корень) конечен;
// запись namespace a.loop остаётся, записей a.loop.* нет (корень — предок).
// Старт с корня и со стартового namespace "a" дают согласованный результат.
func TestRecursiveLoopStillTerminates(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	writeFile(t, filepath.Join(root, "a", "m.yaml"), "name: m\n")
	symlinkOrFail(t, "..", filepath.Join(root, "a", "loop"))
	s := NewGlobalSource(root)

	wantRoot := []string{"module:a.m", "namespace:a", "namespace:a.loop"}
	if got := recursiveEntries(t, s, ""); !slices.Equal(got, wantRoot) {
		t.Errorf("ListChildrenRecursive(\"\") = %v, want %v", got, wantRoot)
	}
	wantA := []string{"module:m", "namespace:loop"}
	if got := recursiveEntries(t, s, "a"); !slices.Equal(got, wantA) {
		t.Errorf("ListChildrenRecursive(\"a\") = %v, want %v", got, wantA)
	}
	names := listNames(t, s)
	if len(names) != 1 || names["a.m"] == "" {
		t.Errorf("List = %v, want только a.m", names)
	}
}

// TestListChildrenRecursiveUnreadableDirPartial: нечитаемый подкаталог не
// роняет и не скрывает обход — результат возвращается с *PartialError.
func TestListChildrenRecursiveUnreadableDirPartial(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.yaml"), "name: a\n")
	writeFile(t, filepath.Join(root, "sub", "b.yaml"), "name: b\n")
	noAccessDir(t, filepath.Join(root, "sub"))
	s := NewGlobalSource(root)

	children, err := s.ListChildrenRecursive("")
	var pe *PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *PartialError", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(root, "sub")) {
		t.Fatalf("err %q не содержит путь sub", err)
	}
	want := []string{"a:module", "sub:namespace"}
	if got := childKeys(children); !reflect.DeepEqual(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}

	modules, err := s.List()
	if !errors.As(err, &pe) {
		t.Fatalf("List err = %v, want *PartialError", err)
	}
	if len(modules) != 1 || modules[0].Name != "a" {
		t.Fatalf("List = %+v, want [a]", modules)
	}
}

// TestListingsUnreadableRootPartial: нечитаемый корень — пустой результат и
// *PartialError; отсутствующий корень — nil, nil (свежая установка).
func TestListingsUnreadableRootPartial(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.yaml"), "name: a\n")
	noAccessDir(t, root)
	s := NewGlobalSource(root)

	children, err := s.ListChildrenRecursive("")
	var pe *PartialError
	if !errors.As(err, &pe) || len(children) != 0 {
		t.Fatalf("ListChildrenRecursive = %+v, %v; want пусто и *PartialError", children, err)
	}
	modules, err := s.List()
	if !errors.As(err, &pe) || len(modules) != 0 {
		t.Fatalf("List = %+v, %v; want пусто и *PartialError", modules, err)
	}
}

func TestListingsMissingRootNoError(t *testing.T) {
	s := NewGlobalSource(filepath.Join(t.TempDir(), "nope"))
	if children, err := s.ListChildrenRecursive(""); children != nil || err != nil {
		t.Fatalf("ListChildrenRecursive = %+v, %v; want nil, nil", children, err)
	}
	if modules, err := s.List(); modules != nil || err != nil {
		t.Fatalf("List = %+v, %v; want nil, nil", modules, err)
	}
}
