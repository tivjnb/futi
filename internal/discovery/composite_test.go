package discovery

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"futi/internal/config"
)

func TestCompositeResolveFromMountpoint(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "deploy.yaml"), "name: deploy\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1.ns2"},
		},
	}
	s := NewCompositeSource(cfg)

	manifest, root, err := s.Resolve("ns1.ns2.deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(dir1, "deploy.yaml") {
		t.Fatalf("unexpected manifest: %q", manifest)
	}
	if root != dir1 {
		t.Fatalf("unexpected root: %q", root)
	}
}

func TestCompositeResolveFromRoot(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1},
		},
	}
	s := NewCompositeSource(cfg)

	manifest, _, err := s.Resolve("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(dir1, "docker.yaml") {
		t.Fatalf("unexpected manifest: %q", manifest)
	}
}

func TestCompositeResolveWithMountAsNamespace(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "dev", MountAsNamespace: "tools"},
		},
	}
	s := NewCompositeSource(cfg)

	manifest, _, err := s.Resolve("dev.tools.docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != filepath.Join(dir1, "docker.yaml") {
		t.Fatalf("unexpected manifest: %q", manifest)
	}
}

func TestCompositeResolveConflictBetweenSources(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "docker.yaml"), "name: docker\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1"},
			{Path: dir2, Mountpoint: "ns1"},
		},
	}
	s := NewCompositeSource(cfg)

	_, _, err := s.Resolve("ns1.docker")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
}

func TestCompositeResolveNotFound(t *testing.T) {
	dir1 := t.TempDir()
	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1"},
		},
	}
	s := NewCompositeSource(cfg)

	_, _, err := s.Resolve("ns1.nonexistent")
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestCompositeResolveNamespaceNotModule(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1.ns2"},
		},
	}
	s := NewCompositeSource(cfg)

	// ns1.ns2 is a namespace (mountpoint), not a module
	_, _, err := s.Resolve("ns1.ns2")
	if err == nil {
		t.Fatal("expected error: namespace is not a module")
	}
}

func TestCompositeListChildrenRootWithMountpoints(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "deploy.yaml"), "name: deploy\n")
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "backup.yaml"), "name: backup\n")
	dir3 := t.TempDir()
	writeFile(t, filepath.Join(dir3, "extra.yaml"), "name: extra\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1.ns2"},
			{Path: dir2, Mountpoint: "ns1.ns2"},
			{Path: dir3, Mountpoint: "ns1"},
		},
	}
	s := NewCompositeSource(cfg)

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child (ns1), got %d: %+v", len(children), children)
	}
	if children[0].Name != "ns1" || children[0].Type != "namespace" {
		t.Fatalf("expected namespace 'ns1', got %+v", children[0])
	}
}

func TestCompositeListChildrenMergedNamespace(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "deploy.yaml"), "name: deploy\n")
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "backup.yaml"), "name: backup\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1.ns2"},
			{Path: dir2, Mountpoint: "ns1.ns2"},
		},
	}
	s := NewCompositeSource(cfg)

	children, err := s.ListChildren("ns1.ns2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(children), children)
	}

	names := make(map[string]bool)
	for _, c := range children {
		names[c.Name] = true
	}
	if !names["deploy"] || !names["backup"] {
		t.Fatalf("expected deploy and backup, got %+v", names)
	}
}

func TestCompositeListChildrenEmptyNamespaceFromMountpoint(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "deploy.yaml"), "name: deploy\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1.ns2"},
		},
	}
	s := NewCompositeSource(cfg)

	// ns1 is an empty namespace (only has ns2 as child)
	children, err := s.ListChildren("ns1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child (ns2), got %d: %+v", len(children), children)
	}
	if children[0].Name != "ns2" || children[0].Type != "namespace" {
		t.Fatalf("expected namespace 'ns2', got %+v", children[0])
	}
}

func TestCompositeListChildrenWithMountAsNamespace(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "dev", MountAsNamespace: "tools"},
		},
	}
	s := NewCompositeSource(cfg)

	// dev has child "tools" (namespace)
	children, err := s.ListChildren("dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].Name != "tools" || children[0].Type != "namespace" {
		t.Fatalf("expected namespace 'tools', got %+v", children[0])
	}

	// dev.tools has child "docker" (module)
	children, err = s.ListChildren("dev.tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].Name != "docker" || children[0].Type != "module" {
		t.Fatalf("expected module 'docker', got %+v", children[0])
	}
}

func TestCompositeListChildrenConflictModuleFromTwoSourcesIsMarked(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "docker.yaml"), "name: docker\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1"},
			{Path: dir2, Mountpoint: "ns1"},
		},
	}
	s := NewCompositeSource(cfg)

	children, err := s.ListChildren("ns1")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(children) != 1 || !children[0].Conflict || !reflect.DeepEqual(children[0].ConflictSources, []string{dir1, dir2}) {
		t.Fatalf("children = %+v, want один модуль docker с Conflict и sources [%s %s]", children, dir1, dir2)
	}
}

func TestCompositeListChildrenRecursiveMerged(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "deploy.yaml"), "name: deploy\n")
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "backup.yaml"), "name: backup\n")
	dir3 := t.TempDir()
	writeFile(t, filepath.Join(dir3, "extra.yaml"), "name: extra\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1, Mountpoint: "ns1.ns2"},
			{Path: dir2, Mountpoint: "ns1.ns2"},
			{Path: dir3, Mountpoint: "ns1"},
		},
	}
	s := NewCompositeSource(cfg)

	children, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byName := make(map[string]string)
	for _, c := range children {
		byName[c.Name] = c.Type
	}

	if byName["ns1"] != "namespace" {
		t.Fatalf("expected namespace 'ns1', got %q", byName["ns1"])
	}
	if byName["ns1.ns2"] != "namespace" {
		t.Fatalf("expected namespace 'ns1.ns2', got %q", byName["ns1.ns2"])
	}
	if byName["ns1.ns2.deploy"] != "module" {
		t.Fatalf("expected module 'ns1.ns2.deploy', got %q", byName["ns1.ns2.deploy"])
	}
	if byName["ns1.ns2.backup"] != "module" {
		t.Fatalf("expected module 'ns1.ns2.backup', got %q", byName["ns1.ns2.backup"])
	}
	if byName["ns1.extra"] != "module" {
		t.Fatalf("expected module 'ns1.extra', got %q", byName["ns1.extra"])
	}
}

func TestCompositeListChildrenFromRootSourceAndMountpointSource(t *testing.T) {
	dir1 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "docker.yaml"), "name: docker\n")
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "git.yaml"), "name: git\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: dir1},                      // root: docker module
			{Path: dir2, Mountpoint: "tools"}, // tools.git module
		},
	}
	s := NewCompositeSource(cfg)

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(children), children)
	}

	byName := make(map[string]string)
	for _, c := range children {
		byName[c.Name] = c.Type
	}
	if byName["docker"] != "module" {
		t.Fatalf("expected module 'docker', got %q", byName["docker"])
	}
	if byName["tools"] != "namespace" {
		t.Fatalf("expected namespace 'tools', got %q", byName["tools"])
	}
}

func TestCompositeSourceResolveConflictNotCounted(t *testing.T) {
	conflict := makeConflictSource(t)
	okDir := t.TempDir()
	mkdir(t, filepath.Join(okDir, "a"))
	writeFile(t, filepath.Join(okDir, "a", "b.yaml"), "name: b\n")

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Path: conflict.RootDir},
			{Path: okDir},
		},
	}
	s := NewCompositeSource(cfg)

	_, _, err := s.Resolve("a.b")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Fatalf("error %q: want the source-level conflict text (both ... exist)", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("conflict must not be ErrNotFound: %v", err)
	}
}

func TestCompositeSourceResolveNotFoundIsErrNotFound(t *testing.T) {
	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: t.TempDir()}}})
	_, _, err := s.Resolve("zzz")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestCompositeListChildrenModuleAndNamespaceFromDifferentSources: модуль tools
// из одного source и namespace tools из другого (в любом порядке sources,
// в т.ч. namespace от mountpoint) видны оба, без ошибки конфликта.
func TestCompositeListChildrenModuleAndNamespaceFromDifferentSources(t *testing.T) {
	moduleDir := t.TempDir()
	nsDir := t.TempDir()
	writeFile(t, filepath.Join(moduleDir, "tools.yaml"), "name: tools\n")
	writeFile(t, filepath.Join(nsDir, "tools", "build.yaml"), "name: build\n")
	mountDir := t.TempDir()
	writeFile(t, filepath.Join(mountDir, "build.yaml"), "name: build\n")

	module := Child{Name: "tools", Type: "module", ManifestPath: filepath.Join(moduleDir, "tools.yaml"), ModuleRoot: moduleDir}
	namespace := Child{Name: "tools", Type: "namespace"}

	tests := []struct {
		name      string
		sources   []config.SourceConfig
		want      []Child
		wantBuild string
	}{
		{
			name:      "модуль, затем namespace",
			sources:   []config.SourceConfig{{Path: moduleDir}, {Path: nsDir}},
			want:      []Child{module, namespace},
			wantBuild: filepath.Join(nsDir, "tools", "build.yaml"),
		},
		{
			name:      "namespace, затем модуль",
			sources:   []config.SourceConfig{{Path: nsDir}, {Path: moduleDir}},
			want:      []Child{namespace, module},
			wantBuild: filepath.Join(nsDir, "tools", "build.yaml"),
		},
		{
			name:      "namespace от mountpoint",
			sources:   []config.SourceConfig{{Path: moduleDir}, {Path: mountDir, Mountpoint: "tools"}},
			want:      []Child{module, namespace},
			wantBuild: filepath.Join(mountDir, "build.yaml"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewCompositeSource(&config.Config{Sources: tt.sources})

			children, err := s.ListChildren("")
			if err != nil {
				t.Fatalf("ListChildren: %v", err)
			}
			if !reflect.DeepEqual(children, tt.want) {
				t.Fatalf("ListChildren(\"\") = %+v, want %+v", children, tt.want)
			}

			manifest, _, err := s.Resolve("tools.build")
			if err != nil {
				t.Fatalf("Resolve(tools.build): %v", err)
			}
			if manifest != tt.wantBuild {
				t.Fatalf("Resolve(tools.build) = %q, want %q", manifest, tt.wantBuild)
			}
		})
	}
}

// TestCompositeListChildrenKeepsConflictBehindNamespace: конфликтный модуль
// (x.yaml + x/config.yaml) одного source не теряется за namespace другого
// и сохраняет пометку Conflict.
func TestCompositeListChildrenKeepsConflictBehindNamespace(t *testing.T) {
	nsDir := t.TempDir()
	conflictDir := t.TempDir()
	writeFile(t, filepath.Join(nsDir, "tools", "build.yaml"), "name: build\n")
	writeFile(t, filepath.Join(conflictDir, "tools.yaml"), "name: tools\n")
	writeFile(t, filepath.Join(conflictDir, "tools", "config.yaml"), "name: tools\n")

	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: nsDir}, {Path: conflictDir}}})

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	want := []Child{
		{Name: "tools", Type: "namespace"},
		{Name: "tools", Type: "module", ManifestPath: filepath.Join(conflictDir, "tools.yaml"), ModuleRoot: conflictDir, Conflict: true},
	}
	if !reflect.DeepEqual(children, want) {
		t.Fatalf("ListChildren(\"\") = %+v, want %+v", children, want)
	}
}

// TestCompositeRecursiveDeepConflictIsMarked: модуль с одним именем из двух
// sources на втором уровне помечается Conflict и в рекурсивном листинге
// (как в ListChildren этого уровня), с префиксом и без.
func TestCompositeRecursiveDeepConflictIsMarked(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir1, "ns", "docker.yaml"), "name: docker\n")
	writeFile(t, filepath.Join(dir2, "ns", "docker.yaml"), "name: docker\n")

	tests := []struct {
		name      string
		sources   []config.SourceConfig
		namespace string
	}{
		{"без префикса, от корня", []config.SourceConfig{{Path: dir1}, {Path: dir2}}, ""},
		{"с префиксом, от корня", []config.SourceConfig{{Path: dir1, Mountpoint: "p"}, {Path: dir2, Mountpoint: "p"}}, ""},
		{"с префиксом, от mountpoint", []config.SourceConfig{{Path: dir1, Mountpoint: "p"}, {Path: dir2, Mountpoint: "p"}}, "p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewCompositeSource(&config.Config{Sources: tt.sources})
			children, err := s.ListChildrenRecursive(tt.namespace)
			if err != nil {
				t.Fatalf("ListChildrenRecursive(%q): %v", tt.namespace, err)
			}
			var got []string
			for _, c := range children {
				if c.Type == "module" && c.Conflict {
					got = append(got, c.Name)
					if !reflect.DeepEqual(c.ConflictSources, []string{dir1, dir2}) {
						t.Fatalf("ConflictSources = %v, want [%s %s]", c.ConflictSources, dir1, dir2)
					}
				}
			}
			if len(got) != 1 {
				t.Fatalf("конфликтных модулей %v, want один (docker): %+v", got, children)
			}
		})
	}
}

// TestCompositeRecursiveMultiSegmentPrefix: source, смонтированный в a.b,
// рядом с корневым source; запросы от корня, от промежуточного сегмента,
// от mountpoint и изнутри source — синтетические namespace для сегментов
// префикса, порядок как у обхода через ListChildren.
func TestCompositeRecursiveMultiSegmentPrefix(t *testing.T) {
	mounted := t.TempDir()
	writeFile(t, filepath.Join(mounted, "top.yaml"), "name: top\n")
	writeFile(t, filepath.Join(mounted, "ns", "m.yaml"), "name: m\n")
	rootDir := t.TempDir()
	writeFile(t, filepath.Join(rootDir, "z.yaml"), "name: z\n")
	writeFile(t, filepath.Join(rootDir, "a", "r.yaml"), "name: r\n")

	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{
		{Path: mounted, Mountpoint: "a.b"},
		{Path: rootDir},
	}})

	m := Child{Type: "module", ManifestPath: filepath.Join(mounted, "ns", "m.yaml"), ModuleRoot: filepath.Join(mounted, "ns")}
	top := Child{Type: "module", ManifestPath: filepath.Join(mounted, "top.yaml"), ModuleRoot: mounted}
	r := Child{Type: "module", ManifestPath: filepath.Join(rootDir, "a", "r.yaml"), ModuleRoot: filepath.Join(rootDir, "a")}
	z := Child{Name: "z", Type: "module", ManifestPath: filepath.Join(rootDir, "z.yaml"), ModuleRoot: rootDir}
	named := func(c Child, name string) Child { c.Name = name; return c }
	ns := func(name string) Child { return Child{Name: name, Type: "namespace"} }

	tests := []struct {
		namespace string
		want      []Child
	}{
		{"", []Child{ns("a"), ns("a.b"), ns("a.b.ns"), named(m, "a.b.ns.m"), named(top, "a.b.top"), named(r, "a.r"), z}},
		{"a", []Child{ns("b"), ns("b.ns"), named(m, "b.ns.m"), named(top, "b.top"), named(r, "r")}},
		{"a.b", []Child{ns("ns"), named(m, "ns.m"), named(top, "top")}},
		{"a.b.ns", []Child{named(m, "m")}},
	}
	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got, err := s.ListChildrenRecursive(tt.namespace)
			if err != nil {
				t.Fatalf("ListChildrenRecursive(%q): %v", tt.namespace, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ListChildrenRecursive(%q) =\n%+v\nwant\n%+v", tt.namespace, got, tt.want)
			}
		})
	}
}

// TestCompositeRecursiveMountedExactlyAtNs: запрос ровно по mountpoint —
// поддерево смонтированного source сливается с одноимённым namespace
// корневого source.
func TestCompositeRecursiveMountedExactlyAtNs(t *testing.T) {
	mounted := t.TempDir()
	writeFile(t, filepath.Join(mounted, "build.yaml"), "name: build\n")
	writeFile(t, filepath.Join(mounted, "sub", "x.yaml"), "name: x\n")
	rootDir := t.TempDir()
	writeFile(t, filepath.Join(rootDir, "tools", "extra.yaml"), "name: extra\n")

	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{
		{Path: mounted, Mountpoint: "tools"},
		{Path: rootDir},
	}})

	got, err := s.ListChildrenRecursive("tools")
	if err != nil {
		t.Fatalf("ListChildrenRecursive(tools): %v", err)
	}
	want := []Child{
		{Name: "build", Type: "module", ManifestPath: filepath.Join(mounted, "build.yaml"), ModuleRoot: mounted},
		{Name: "sub", Type: "namespace"},
		{Name: "sub.x", Type: "module", ManifestPath: filepath.Join(mounted, "sub", "x.yaml"), ModuleRoot: filepath.Join(mounted, "sub")},
		{Name: "extra", Type: "module", ManifestPath: filepath.Join(rootDir, "tools", "extra.yaml"), ModuleRoot: filepath.Join(rootDir, "tools")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListChildrenRecursive(tools) =\n%+v\nwant\n%+v", got, want)
	}
}

// TestCompositeMissingSourcePartial: несуществующий source не роняет листинг —
// модули остальных возвращаются вместе с *PartialError, где назван путь.
func TestCompositeMissingSourcePartial(t *testing.T) {
	a := t.TempDir()
	writeFile(t, filepath.Join(a, "x.yaml"), "name: x\n")
	b := filepath.Join(t.TempDir(), "nope")
	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}, {Path: b}}})

	tests := []struct {
		name string
		list func() ([]Child, error)
	}{
		{"ListChildren", func() ([]Child, error) { return s.ListChildren("") }},
		{"ListChildrenRecursive", func() ([]Child, error) { return s.ListChildrenRecursive("") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			children, err := tt.list()
			var pe *PartialError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want *PartialError", err)
			}
			if !strings.Contains(err.Error(), b) {
				t.Fatalf("err %q не содержит путь %q", err, b)
			}
			want := []Child{{Name: "x", Type: "module", ManifestPath: filepath.Join(a, "x.yaml"), ModuleRoot: a}}
			if !reflect.DeepEqual(children, want) {
				t.Fatalf("children = %+v, want %+v", children, want)
			}
		})
	}

	t.Run("List", func(t *testing.T) {
		modules, err := s.List()
		var pe *PartialError
		if !errors.As(err, &pe) || !strings.Contains(err.Error(), b) {
			t.Fatalf("err = %v, want *PartialError с путём %q", err, b)
		}
		if len(modules) != 1 || modules[0].Name != "x" {
			t.Fatalf("modules = %+v, want [x]", modules)
		}
	})
}

// TestCompositeSourceIsFilePartial: source — файл, а не каталог: ошибка
// «not a directory», синтетические namespace для него не создаются.
func TestCompositeSourceIsFilePartial(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	writeFile(t, f, "x")
	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: f, Mountpoint: "m"}}})

	children, err := s.ListChildren("")
	var pe *PartialError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v, want *PartialError not a directory", err)
	}
	if len(children) != 0 {
		t.Fatalf("children = %+v, want пусто (без namespace m)", children)
	}
}

// TestCompositeConflictMarked: модуль из нескольких sources помечается
// Conflict с перечнем RootDir; листинг не падает, Resolve — ошибка.
func TestCompositeConflictMarked(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeFile(t, filepath.Join(a, "x.yaml"), "name: x\n")
	writeFile(t, filepath.Join(b, "x.yaml"), "name: x\n")
	writeFile(t, filepath.Join(a, "ns", "y.yaml"), "name: y\n")
	writeFile(t, filepath.Join(b, "ns", "y.yaml"), "name: y\n")

	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}, {Path: b}}})

	children, err := s.ListChildren("")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	want := []Child{
		{Name: "ns", Type: "namespace"},
		{Name: "x", Type: "module", ManifestPath: filepath.Join(a, "x.yaml"), ModuleRoot: a, Conflict: true, ConflictSources: []string{a, b}},
	}
	if !reflect.DeepEqual(children, want) {
		t.Fatalf("ListChildren = %+v, want %+v", children, want)
	}

	rec, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("ListChildrenRecursive: %v", err)
	}
	wantRec := []Child{
		{Name: "ns", Type: "namespace"},
		{Name: "ns.y", Type: "module", ManifestPath: filepath.Join(a, "ns", "y.yaml"), ModuleRoot: filepath.Join(a, "ns"), Conflict: true, ConflictSources: []string{a, b}},
		{Name: "x", Type: "module", ManifestPath: filepath.Join(a, "x.yaml"), ModuleRoot: a, Conflict: true, ConflictSources: []string{a, b}},
	}
	if !reflect.DeepEqual(rec, wantRec) {
		t.Fatalf("ListChildrenRecursive = %+v, want %+v", rec, wantRec)
	}

	_, _, err = s.Resolve("x")
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("Resolve(x) err = %v, want conflict", err)
	}
}

// TestCompositeConflictMarkedWithMountpoint: конфликт под общим mountpoint
// помечается в рекурсивном листинге с полным именем.
func TestCompositeConflictMarkedWithMountpoint(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeFile(t, filepath.Join(a, "x.yaml"), "name: x\n")
	writeFile(t, filepath.Join(b, "x.yaml"), "name: x\n")
	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a, Mountpoint: "ns"}, {Path: b, Mountpoint: "ns"}}})

	rec, err := s.ListChildrenRecursive("")
	if err != nil {
		t.Fatal(err)
	}
	want := []Child{
		{Name: "ns", Type: "namespace"},
		{Name: "ns.x", Type: "module", ManifestPath: filepath.Join(a, "x.yaml"), ModuleRoot: a, Conflict: true, ConflictSources: []string{a, b}},
	}
	if !reflect.DeepEqual(rec, want) {
		t.Fatalf("got %+v, want %+v", rec, want)
	}
}

// TestCompositeListChildrenNotFound: namespace, которого нет ни в одном
// source, — ErrNotFound; корень — никогда.
func TestCompositeListChildrenNotFound(t *testing.T) {
	a := t.TempDir()
	writeFile(t, filepath.Join(a, "x.yaml"), "name: x\n")
	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}}})

	if _, err := s.ListChildren("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListChildren(nope) err = %v, want ErrNotFound", err)
	}
	if _, err := s.ListChildrenRecursive("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListChildrenRecursive(nope) err = %v, want ErrNotFound", err)
	}
	empty := NewCompositeSource(&config.Config{})
	if _, err := empty.ListChildren(""); err != nil {
		t.Fatalf("ListChildren(\"\") без sources: %v", err)
	}
	if _, err := empty.ListChildrenRecursive(""); err != nil {
		t.Fatalf("ListChildrenRecursive(\"\") без sources: %v", err)
	}
}

// TestCompositeNotFoundWithBrokenSourceIsPlainError: namespace не найден, а
// source недоступен — результата нет, ошибка обычная (не *PartialError) и не
// ErrNotFound: namespace мог быть в недоступном source.
func TestCompositeNotFoundWithBrokenSourceIsPlainError(t *testing.T) {
	a := t.TempDir()
	b := filepath.Join(t.TempDir(), "nope")
	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}, {Path: b}}})

	_, err := s.ListChildren("zzz")
	var pe *PartialError
	if err == nil || errors.As(err, &pe) || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), b) {
		t.Fatalf("err = %v, want обычная ошибка с путём %q", err, b)
	}
}

// TestCompositeInvalidNamespaceIsPlainError: ошибка делегата без результата
// от других sources — обычная ошибка, не *PartialError.
func TestCompositeInvalidNamespaceIsPlainError(t *testing.T) {
	a := t.TempDir()
	writeFile(t, filepath.Join(a, "x.yaml"), "name: x\n")
	s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}}})

	tests := []struct {
		name string
		list func() ([]Child, error)
	}{
		{"ListChildren", func() ([]Child, error) { return s.ListChildren("a..b") }},
		{"ListChildrenRecursive", func() ([]Child, error) { return s.ListChildrenRecursive("a..b") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.list()
			var pe *PartialError
			if err == nil || errors.As(err, &pe) {
				t.Fatalf("err = %v, want обычная ошибка (не *PartialError)", err)
			}
		})
	}
}

// TestCompositeUnreadableNamespaceDir: каталог namespace без прав в одном из
// sources — при результате от другого source это *PartialError с детьми,
// без него — обычная ошибка.
func TestCompositeUnreadableNamespaceDir(t *testing.T) {
	a := t.TempDir()
	writeFile(t, filepath.Join(a, "ns", "m.yaml"), "name: m\n")
	b := t.TempDir()
	mkdir(t, filepath.Join(b, "ns"))
	noAccessDir(t, filepath.Join(b, "ns"))

	t.Run("есть результат от другого source", func(t *testing.T) {
		s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}, {Path: b}}})
		children, err := s.ListChildren("ns")
		var pe *PartialError
		if !errors.As(err, &pe) {
			t.Fatalf("err = %v, want *PartialError", err)
		}
		if len(children) != 1 || children[0].Name != "m" {
			t.Fatalf("children = %+v, want [m]", children)
		}
	})
	t.Run("единственный source", func(t *testing.T) {
		s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: b}}})
		_, err := s.ListChildren("ns")
		var pe *PartialError
		if err == nil || errors.As(err, &pe) {
			t.Fatalf("err = %v, want обычная ошибка (не *PartialError)", err)
		}
	})
}

// TestCompositeUnreadableRootSource: единственный source с chmod 000 в корне
// (ns == "") — пустой результат и *PartialError; тот же source, смонтированный
// в ns, при запросе ns — обычная ошибка (результата нет), одинаково для
// ListChildren и ListChildrenRecursive.
func TestCompositeUnreadableRootSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "m.yaml"), "name: m\n")
	noAccessDir(t, root)

	t.Run("корень", func(t *testing.T) {
		s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: root}}})
		for name, list := range map[string]func(string) ([]Child, error){"ListChildren": s.ListChildren, "ListChildrenRecursive": s.ListChildrenRecursive} {
			children, err := list("")
			var pe *PartialError
			if !errors.As(err, &pe) || len(children) != 0 {
				t.Fatalf("%s(\"\") = %+v, %v; want пусто и *PartialError", name, children, err)
			}
		}
	})
	t.Run("смонтирован в ns", func(t *testing.T) {
		s := NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: root, Mountpoint: "ns"}}})
		for name, list := range map[string]func(string) ([]Child, error){"ListChildren": s.ListChildren, "ListChildrenRecursive": s.ListChildrenRecursive} {
			_, err := list("ns")
			var pe *PartialError
			if err == nil || errors.As(err, &pe) {
				t.Fatalf("%s(ns) err = %v, want обычная ошибка (не *PartialError)", name, err)
			}
		}
	})
}
