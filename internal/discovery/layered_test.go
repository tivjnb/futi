package discovery

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"futi/internal/config"
)

func TestFileSourceResolveByModuleName(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "futi.yaml")
	writeFile(t, manifestPath, "name: local\ncommands:\n  greet:\n    cmd: echo hi\n")

	s := NewFileSource(manifestPath, dir, "local")
	manifest, root, err := s.Resolve("local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != manifestPath {
		t.Fatalf("unexpected manifest: %q, want %q", manifest, manifestPath)
	}
	if root != dir {
		t.Fatalf("unexpected root: %q, want %q", root, dir)
	}
}

func TestFileSourceResolveNotFound(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "futi.yaml")
	writeFile(t, manifestPath, "name: local\n")

	s := NewFileSource(manifestPath, dir, "local")
	_, _, err := s.Resolve("other")
	if err == nil {
		t.Fatal("expected error for non-matching namespace")
	}
}

func TestFileSourceListChildrenRoot(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "futi.yaml")
	writeFile(t, manifestPath, "name: local\n")

	s := NewFileSource(manifestPath, dir, "local")
	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].Name != "local" || children[0].Type != "module" {
		t.Fatalf("expected module 'local', got %+v", children[0])
	}
	if children[0].ManifestPath != manifestPath {
		t.Fatalf("unexpected manifest path: %q", children[0].ManifestPath)
	}
}

func TestFileSourceListChildrenNamespaceNotFound(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "futi.yaml")
	writeFile(t, manifestPath, "name: local\n")

	s := NewFileSource(manifestPath, dir, "local")
	_, err := s.ListChildren("local")
	if err == nil {
		t.Fatal("expected error: 'local' is a module, not a namespace")
	}
}

func TestFileSourceList(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "futi.yaml")
	writeFile(t, manifestPath, "name: local\n")

	s := NewFileSource(manifestPath, dir, "local")
	modules, err := s.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Name != "local" {
		t.Fatalf("expected 'local', got %q", modules[0].Name)
	}
	if modules[0].ManifestPath != manifestPath {
		t.Fatalf("unexpected manifest path: %q", modules[0].ManifestPath)
	}
}

func TestLayeredSourceResolvePrimaryWins(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "docker.yaml"), "name: docker\n")

	primary := NewGlobalSource(dir1)
	fallback := NewGlobalSource(dir2)
	s := NewLayeredSource(primary, fallback)

	manifest, _, err := s.Resolve("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(dir1, "docker.yaml") {
		t.Fatalf("expected primary manifest, got %q", manifest)
	}
}

func TestLayeredSourceResolveFallback(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "git.yaml"), "name: git\n")

	primary := NewGlobalSource(dir1)
	fallback := NewGlobalSource(dir2)
	s := NewLayeredSource(primary, fallback)

	manifest, _, err := s.Resolve("git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(dir2, "git.yaml") {
		t.Fatalf("expected fallback manifest, got %q", manifest)
	}
}

func TestLayeredSourceResolveNotFound(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	primary := NewGlobalSource(dir1)
	fallback := NewGlobalSource(dir2)
	s := NewLayeredSource(primary, fallback)

	_, _, err := s.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}

func TestLayeredSourceListChildrenMerges(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "git.yaml"), "name: git\n")

	primary := NewGlobalSource(dir1)
	fallback := NewGlobalSource(dir2)
	s := NewLayeredSource(primary, fallback)

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(children), children)
	}

	names := make(map[string]string)
	for _, c := range children {
		names[c.Name] = c.Type
	}
	if names["docker"] != "module" {
		t.Fatalf("expected module 'docker', got %q", names["docker"])
	}
	if names["git"] != "module" {
		t.Fatalf("expected module 'git', got %q", names["git"])
	}
}

func TestLayeredSourceListChildrenPrimaryOverrides(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "git.yaml"), "name: git\n")

	primary := NewGlobalSource(dir1)
	fallback := NewGlobalSource(dir2)
	s := NewLayeredSource(primary, fallback)

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children (docker from primary, git from fallback), got %d: %+v", len(children), children)
	}

	byName := make(map[string]Child)
	for _, c := range children {
		byName[c.Name] = c
	}
	if c, ok := byName["docker"]; !ok || c.ManifestPath != filepath.Join(dir1, "docker.yaml") {
		t.Fatalf("expected docker from primary, got %+v", c)
	}
	if _, ok := byName["git"]; !ok {
		t.Fatal("expected git from fallback")
	}
}

func TestLayeredSourceListChildrenRecursive(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	nsDir := filepath.Join(dir2, "images")
	mkdir(t, nsDir)
	writeFile(t, filepath.Join(nsDir, "png.yaml"), "name: images.png\n")

	primary := NewGlobalSource(dir1)
	fallback := NewGlobalSource(dir2)
	s := NewLayeredSource(primary, fallback)

	children, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
}

// TestLayeredRecursiveSameAsBeforeNoCycles: на дереве без циклов рекурсивный
// листинг двух слоёв совпадает с прежним (рекурсия через ListChildren):
// дети каждого namespace — сначала верхнего слоя, затем новые ключи нижнего,
// обход в глубину; пара (имя, тип) берётся из верхнего слоя, Conflict
// сохраняется.
func TestLayeredRecursiveSameAsBeforeNoCycles(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir1, "tools.yaml"), "name: tools\n")
	writeFile(t, filepath.Join(dir1, "dev", "deploy.yaml"), "name: deploy\n")
	writeFile(t, filepath.Join(dir1, "dev", "sub", "x.yaml"), "name: x\n")

	writeFile(t, filepath.Join(dir2, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "dev", "build.yaml"), "name: build\n")
	writeFile(t, filepath.Join(dir2, "dev", "c.yaml"), "name: c\n")
	writeFile(t, filepath.Join(dir2, "dev", "c", "config.yaml"), "name: c\n")
	writeFile(t, filepath.Join(dir2, "dev", "sub", "y.yaml"), "name: y\n")
	writeFile(t, filepath.Join(dir2, "tools", "build.yaml"), "name: build\n")
	writeFile(t, filepath.Join(dir2, "pkg", "config.yaml"), "name: pkg\n")
	writeFile(t, filepath.Join(dir2, "images", "png.yaml"), "name: png\n")

	s := NewLayeredSource(NewGlobalSource(dir1), NewGlobalSource(dir2))

	got, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("ListChildrenRecursive: %v", err)
	}
	want := []Child{
		{Name: "dev", Type: "namespace"},
		{Name: "dev.deploy", Type: "module", ManifestPath: filepath.Join(dir1, "dev", "deploy.yaml"), ModuleRoot: filepath.Join(dir1, "dev")},
		{Name: "dev.sub", Type: "namespace"},
		{Name: "dev.sub.x", Type: "module", ManifestPath: filepath.Join(dir1, "dev", "sub", "x.yaml"), ModuleRoot: filepath.Join(dir1, "dev", "sub")},
		{Name: "dev.sub.y", Type: "module", ManifestPath: filepath.Join(dir2, "dev", "sub", "y.yaml"), ModuleRoot: filepath.Join(dir2, "dev", "sub")},
		{Name: "dev.build", Type: "module", ManifestPath: filepath.Join(dir2, "dev", "build.yaml"), ModuleRoot: filepath.Join(dir2, "dev")},
		{Name: "dev.c", Type: "module", ManifestPath: filepath.Join(dir2, "dev", "c.yaml"), ModuleRoot: filepath.Join(dir2, "dev"), Conflict: true},
		{Name: "docker", Type: "module", ManifestPath: filepath.Join(dir1, "docker.yaml"), ModuleRoot: dir1},
		{Name: "tools", Type: "module", ManifestPath: filepath.Join(dir1, "tools.yaml"), ModuleRoot: dir1},
		{Name: "images", Type: "namespace"},
		{Name: "images.png", Type: "module", ManifestPath: filepath.Join(dir2, "images", "png.yaml"), ModuleRoot: filepath.Join(dir2, "images")},
		{Name: "pkg", Type: "module", ManifestPath: filepath.Join(dir2, "pkg", "config.yaml"), ModuleRoot: filepath.Join(dir2, "pkg")},
		{Name: "tools", Type: "namespace"},
		{Name: "tools.build", Type: "module", ManifestPath: filepath.Join(dir2, "tools", "build.yaml"), ModuleRoot: filepath.Join(dir2, "tools")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListChildrenRecursive(\"\") =\n%+v\nwant\n%+v", got, want)
	}

	got, err = s.ListChildrenRecursive("dev")
	if err != nil {
		t.Fatalf("ListChildrenRecursive(dev): %v", err)
	}
	wantDev := []Child{
		{Name: "deploy", Type: "module", ManifestPath: filepath.Join(dir1, "dev", "deploy.yaml"), ModuleRoot: filepath.Join(dir1, "dev")},
		{Name: "sub", Type: "namespace"},
		{Name: "sub.x", Type: "module", ManifestPath: filepath.Join(dir1, "dev", "sub", "x.yaml"), ModuleRoot: filepath.Join(dir1, "dev", "sub")},
		{Name: "sub.y", Type: "module", ManifestPath: filepath.Join(dir2, "dev", "sub", "y.yaml"), ModuleRoot: filepath.Join(dir2, "dev", "sub")},
		{Name: "build", Type: "module", ManifestPath: filepath.Join(dir2, "dev", "build.yaml"), ModuleRoot: filepath.Join(dir2, "dev")},
		{Name: "c", Type: "module", ManifestPath: filepath.Join(dir2, "dev", "c.yaml"), ModuleRoot: filepath.Join(dir2, "dev"), Conflict: true},
	}
	if !reflect.DeepEqual(got, wantDev) {
		t.Fatalf("ListChildrenRecursive(dev) =\n%+v\nwant\n%+v", got, wantDev)
	}
}

// nameType — имя и тип записи листинга (без путей) для сравнения множеств.
type nameType struct{ name, typ string }

func nameTypeSet(children []Child) map[nameType]int {
	set := make(map[nameType]int)
	for _, c := range children {
		set[nameType{c.Name, c.Type}]++
	}
	return set
}

// TestLayeredRecursiveLoopMatchesGlobal: симлинк a/loop -> .. (цикл) —
// Layered и Composite (mount без префикса и с префиксом) дают то же
// конечное множество (имя, тип), что и сам GlobalSource, без мусора
// из повторного обхода через цикл.
func TestLayeredRecursiveLoopMatchesGlobal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "top.yaml"), "name: top\n")
	writeFile(t, filepath.Join(root, "a", "m.yaml"), "name: m\n")
	symlinkOrFail(t, "..", filepath.Join(root, "a", "loop"))

	want := map[nameType]int{
		{"a", "namespace"}:      1,
		{"a.loop", "namespace"}: 1,
		{"a.m", "module"}:       1,
		{"top", "module"}:       1,
	}
	wantPrefixed := map[nameType]int{
		{"p", "namespace"}:        1,
		{"p.a", "namespace"}:      1,
		{"p.a.loop", "namespace"}: 1,
		{"p.a.m", "module"}:       1,
		{"p.top", "module"}:       1,
	}

	global := NewGlobalSource(root)
	plain := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: root}}})
	mounted := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: root, Mountpoint: "p"}}})

	tests := []struct {
		name string
		src  Source
		want map[nameType]int
	}{
		{"global", global, want},
		{"layered(global)", NewLayeredSource(global), want},
		{"composite без префикса", plain, want},
		{"composite с префиксом p", mounted, wantPrefixed},
		{"layered(composite)", NewLayeredSource(plain), want},
		{"layered(composite p)", NewLayeredSource(mounted), wantPrefixed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			children, err := tt.src.ListChildrenRecursive("")
			if err != nil {
				t.Fatalf("ListChildrenRecursive: %v", err)
			}
			if got := nameTypeSet(children); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ListChildrenRecursive(\"\"): %d записей (%d разных пар), want %v", len(children), len(got), tt.want)
			}
		})
	}
}

// TestLayeredRecursiveLayerErrors фиксирует решение об ошибках слоёв:
// слой, у которого нет запрошенного namespace, пропускается, если другой
// слой его листит (как в ListChildren); единственный слой — ошибка
// пробрасывается; ошибка внутри поддерева слоя (конфликт модуля из двух
// sources в Composite на глубине) пробрасывается всегда — иначе пропал бы
// весь слой, а конфликт остался бы незамеченным.
func TestLayeredRecursiveLayerErrors(t *testing.T) {
	onlyNs := t.TempDir()
	writeFile(t, filepath.Join(onlyNs, "ns", "m.yaml"), "name: m\n")

	conflictDir1 := t.TempDir()
	conflictDir2 := t.TempDir()
	writeFile(t, filepath.Join(conflictDir1, "ns", "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(conflictDir2, "ns", "docker.yaml"), "name: docker\n")
	conflicting := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: conflictDir1}, {Path: conflictDir2}}})

	local := t.TempDir()
	writeFile(t, filepath.Join(local, "futi.yaml"), "name: local\n")
	fileSrc := NewFileSource(filepath.Join(local, "futi.yaml"), local, "local")

	tests := []struct {
		name      string
		src       Source
		namespace string
		wantErr   bool
	}{
		{"namespace только в нижнем слое", NewLayeredSource(NewGlobalSource(t.TempDir()), NewGlobalSource(onlyNs)), "ns", false},
		{"единственный слой без namespace", NewLayeredSource(NewGlobalSource(onlyNs)), "nope", true},
		{"единственный слой с конфликтом", NewLayeredSource(conflicting), "", true},
		{"FileSource поверх конфликта", NewLayeredSource(fileSrc, conflicting), "", true},
		{"конфликт за namespace другого слоя", NewLayeredSource(NewGlobalSource(onlyNs), conflicting), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.src.ListChildrenRecursive(tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListChildrenRecursive(%q) err = %v, wantErr %v", tt.namespace, err, tt.wantErr)
			}
		})
	}
}

// makeConflictSource создаёт источник с конфликтной парой a/b.yaml + a/b/config.yaml.
func makeConflictSource(t *testing.T) *GlobalSource {
	t.Helper()
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "a", "b"))
	writeFile(t, filepath.Join(dir, "a", "b.yaml"), "name: b\n")
	writeFile(t, filepath.Join(dir, "a", "b", "config.yaml"), "name: b\n")
	return NewGlobalSource(dir)
}

func TestLayeredSourceResolveConflictNotSwallowed(t *testing.T) {
	primary := makeConflictSource(t)
	fbDir := t.TempDir()
	mkdir(t, filepath.Join(fbDir, "a"))
	writeFile(t, filepath.Join(fbDir, "a", "b.yaml"), "name: b\n")
	fallback := NewGlobalSource(fbDir)

	s := NewLayeredSource(primary, fallback)

	_, _, err := s.Resolve("a.b")
	if err == nil {
		t.Fatal("expected conflict error, got fallback result")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error %q does not mention conflict", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("conflict must not be ErrNotFound: %v", err)
	}
}

func TestLayeredSourceResolveNotFoundIsErrNotFound(t *testing.T) {
	s := NewLayeredSource(NewGlobalSource(t.TempDir()))
	_, _, err := s.Resolve("zzz")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLayeredOverCompositeConflictPropagates(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "a"))
	writeFile(t, filepath.Join(dir, "a.yaml"), "name: a\n")
	writeFile(t, filepath.Join(dir, "a", "config.yaml"), "name: a\n")
	primary := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: dir}}})

	fbDir := t.TempDir()
	writeFile(t, filepath.Join(fbDir, "a.yaml"), "name: a\n")

	_, _, err := NewLayeredSource(primary, NewGlobalSource(fbDir)).Resolve("a")
	if err == nil {
		t.Fatal("ожидалась ошибка конфликта, получен результат fallback")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error %q does not mention conflict", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("конфликт не должен быть ErrNotFound: %v", err)
	}
}

// TestLayeredListChildrenKeepsNamespaceBehindModule: модуль tools из верхнего
// слоя не прячет namespace tools нижнего слоя — оба видны, как и в Resolve.
func TestLayeredListChildrenKeepsNamespaceBehindModule(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "tools.yaml"), "name: tools\n")
	writeFile(t, filepath.Join(dir2, "tools", "build.yaml"), "name: build\n")

	s := NewLayeredSource(NewGlobalSource(dir1), NewGlobalSource(dir2))

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	want := []Child{
		{Name: "tools", Type: "module", ManifestPath: filepath.Join(dir1, "tools.yaml"), ModuleRoot: dir1},
		{Name: "tools", Type: "namespace"},
	}
	if !reflect.DeepEqual(children, want) {
		t.Fatalf("ListChildren(\"\") = %+v, want %+v", children, want)
	}

	manifest, _, err := s.Resolve("tools.build")
	if err != nil {
		t.Fatalf("Resolve(tools.build): %v", err)
	}
	if want := filepath.Join(dir2, "tools", "build.yaml"); manifest != want {
		t.Fatalf("Resolve(tools.build) = %q, want %q", manifest, want)
	}
}

// TestLayeredListKeepsPriorityWithinSameKey: одинаковая пара (имя, тип)
// берётся из верхнего слоя; запись другого типа с тем же именем остаётся.
func TestLayeredListKeepsPriorityWithinSameKey(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "tools.yaml"), "name: tools\n")
	writeFile(t, filepath.Join(dir2, "tools.yaml"), "name: tools\n")
	writeFile(t, filepath.Join(dir2, "tools", "build.yaml"), "name: build\n")

	s := NewLayeredSource(NewGlobalSource(dir1), NewGlobalSource(dir2))

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	wantChildren := []Child{
		{Name: "tools", Type: "module", ManifestPath: filepath.Join(dir1, "tools.yaml"), ModuleRoot: dir1},
		{Name: "tools", Type: "namespace"},
	}
	if !reflect.DeepEqual(children, wantChildren) {
		t.Fatalf("ListChildren(\"\") = %+v, want %+v", children, wantChildren)
	}

	modules, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make(map[string]string)
	for _, m := range modules {
		if _, dup := got[m.Name]; dup {
			t.Fatalf("List: модуль %q повторяется: %+v", m.Name, modules)
		}
		got[m.Name] = m.ManifestPath
	}
	wantModules := map[string]string{
		"tools":       filepath.Join(dir1, "tools.yaml"),
		"tools.build": filepath.Join(dir2, "tools", "build.yaml"),
	}
	if !reflect.DeepEqual(got, wantModules) {
		t.Fatalf("List = %v, want %v", got, wantModules)
	}
}

// TestLayeredListChildrenKeepsConflictBehindNamespace: конфликтный модуль
// нижнего слоя (x.yaml + x/config.yaml) не теряется за namespace верхнего
// и сохраняет пометку Conflict.
func TestLayeredListChildrenKeepsConflictBehindNamespace(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "tools", "build.yaml"), "name: build\n")
	writeFile(t, filepath.Join(dir2, "tools.yaml"), "name: tools\n")
	writeFile(t, filepath.Join(dir2, "tools", "config.yaml"), "name: tools\n")

	s := NewLayeredSource(NewGlobalSource(dir1), NewGlobalSource(dir2))

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	want := []Child{
		{Name: "tools", Type: "namespace"},
		{Name: "tools", Type: "module", ManifestPath: filepath.Join(dir2, "tools.yaml"), ModuleRoot: dir2, Conflict: true},
	}
	if !reflect.DeepEqual(children, want) {
		t.Fatalf("ListChildren(\"\") = %+v, want %+v", children, want)
	}
}
