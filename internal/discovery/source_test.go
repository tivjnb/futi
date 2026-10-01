package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
// Семантика: каждый реальный каталог обходится один раз — первым
// встреченным путём (при сортировке a < b содержимое b доступно как
// a.loop.*, прямой путь b уже «использован» через a.loop).
// Дубликатов модулей по ManifestPath нет.
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

// TestListBrokenSymlinkKeptAsFile: битый симлинк на .yaml трактуется как
// файл-модуль (цель может стать доступной позже; кэш сохраняет запись при
// временной ошибке Stat — см. TestCacheRefreshDoesNotRemoveEntriesWithStatErrors).
func TestListBrokenSymlinkKeptAsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real.yaml"), "name: real\n")
	symlinkOrFail(t, filepath.Join(dir, "nonexistent.yaml"), filepath.Join(dir, "broken.yaml"))

	s := NewGlobalSource(dir)
	names := listNames(t, s)
	if _, ok := names["real"]; !ok {
		t.Fatalf("missing real module, got %v", names)
	}
	if _, ok := names["broken"]; !ok {
		t.Fatalf("expected broken .yaml symlink to be kept as file module, got %v", names)
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
