package discovery

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
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
