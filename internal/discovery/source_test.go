package discovery

import (
	"os"
	"path/filepath"
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
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
