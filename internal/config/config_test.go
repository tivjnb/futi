package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesSources(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
sources:
  - path: /home/user/.config/futi/modules
  - path: /path/to/project
    mountpoint: ns1.ns2
  - path: /path/to/tools
    mountpoint: dev
    mount_as_namespace: utilities
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if len(cfg.Sources) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(cfg.Sources))
	}

	if cfg.Sources[0].Path != "/home/user/.config/futi/modules" {
		t.Fatalf("unexpected path[0]: %q", cfg.Sources[0].Path)
	}
	if cfg.Sources[0].Mountpoint != "" || cfg.Sources[0].MountAsNamespace != "" {
		t.Fatalf("expected empty mountpoint and mount_as_namespace for source[0]")
	}

	if cfg.Sources[1].Mountpoint != "ns1.ns2" {
		t.Fatalf("unexpected mountpoint[1]: %q", cfg.Sources[1].Mountpoint)
	}

	if cfg.Sources[2].Mountpoint != "dev" || cfg.Sources[2].MountAsNamespace != "utilities" {
		t.Fatalf("unexpected source[2]: %+v", cfg.Sources[2])
	}
}

func TestLoadNonExistentReturnsNil(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for non-existent file, got %+v", cfg)
	}
}

func TestLoadInvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `sources: [\n  invalid`)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadEmptyPathReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
sources:
  - path: ""
`)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFullPrefix(t *testing.T) {
	tests := []struct {
		name string
		sc   SourceConfig
		want string
	}{
		{"no mount", SourceConfig{Path: "/foo"}, ""},
		{"mountpoint only", SourceConfig{Path: "/foo", Mountpoint: "ns1.ns2"}, "ns1.ns2"},
		{"mount_as_namespace only", SourceConfig{Path: "/foo", MountAsNamespace: "tools"}, "tools"},
		{"both", SourceConfig{Path: "/foo", Mountpoint: "dev", MountAsNamespace: "tools"}, "dev.tools"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sc.FullPrefix(); got != tt.want {
				t.Fatalf("FullPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadTrimsDots(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
sources:
  - path: /foo
    mountpoint: ".ns1.ns2."
    mount_as_namespace: ".tools."
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Sources[0].Mountpoint != "ns1.ns2" {
		t.Fatalf("expected trimmed mountpoint, got %q", cfg.Sources[0].Mountpoint)
	}
	if cfg.Sources[0].MountAsNamespace != "tools" {
		t.Fatalf("expected trimmed mount_as_namespace, got %q", cfg.Sources[0].MountAsNamespace)
	}
}

// TestLoadParsesCacheSection проверяет, что секция cache парсится корректно.
func TestLoadParsesCacheSection(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
sources:
  - path: /home/user/.config/futi/modules
cache:
  refresh: every_t_min
  time: 30m
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Cache.Refresh != "every_t_min" {
		t.Fatalf("expected refresh=every_t_min, got %q", cfg.Cache.Refresh)
	}
	if cfg.Cache.Time != "30m" {
		t.Fatalf("expected time=30m, got %q", cfg.Cache.Time)
	}
}

// TestLoadCacheSectionMissingDefaultsToEmpty проверяет, что отсутствующая
// секция cache → zero-value CacheConfig (Refresh="", Time="").
// NewPolicyFromConfig("", "") → AlwaysPolicy (default).
func TestLoadCacheSectionMissingDefaultsToEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
sources:
  - path: /home/user/.config/futi/modules
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Cache.Refresh != "" {
		t.Fatalf("expected empty refresh, got %q", cfg.Cache.Refresh)
	}
	if cfg.Cache.Time != "" {
		t.Fatalf("expected empty time, got %q", cfg.Cache.Time)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}
