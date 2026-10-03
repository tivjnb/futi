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

func TestCompositeListChildrenConflictModuleFromTwoSources(t *testing.T) {
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

	_, err := s.ListChildren("ns1")
	if err == nil {
		t.Fatal("expected conflict error")
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

// TestCompositeRecursiveDeepConflictIsError: модуль с одним именем из двух
// sources на втором уровне — ошибка конфликта и в рекурсивном листинге
// (как в ListChildren этого уровня и в Resolve), с префиксом и без.
func TestCompositeRecursiveDeepConflictIsError(t *testing.T) {
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
			if err == nil {
				t.Fatalf("ListChildrenRecursive(%q) = %+v, ожидалась ошибка конфликта", tt.namespace, children)
			}
			if !strings.Contains(err.Error(), "conflict") {
				t.Fatalf("error %q does not mention conflict", err)
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
