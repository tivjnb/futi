package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config — главный конфиг futi.
type Config struct {
	Sources []SourceConfig `yaml:"sources"`
	Cache   CacheConfig    `yaml:"cache"`
}

// CacheConfig — настройки кэша модулей.
//
// Refresh — стратегия обновления:
//   - "every_time" (default) — обновлять при каждом запуске CLI.
//   - "every_t_min" — обновлять раз в Time (см. ниже).
//   - "force_only" — никогда автоматически, только `futi cache refresh`.
//
// Time — длительность для "every_t_min". Формат: "30m", "2h", "1d4h".
// Игнорируется для "every_time" и "force_only".
//
// Пустая секция (отсутствует в config.yaml) → zero-value → AlwaysPolicy (default).
// Валидация значений — в modulecache.NewPolicyFromConfig.
type CacheConfig struct {
	Refresh string `yaml:"refresh"`
	Time    string `yaml:"time"`
}

// SourceConfig описывает один источник модулей.
type SourceConfig struct {
	Path             string `yaml:"path"`
	Mountpoint       string `yaml:"mountpoint"`
	MountAsNamespace string `yaml:"mount_as_namespace"`
}

// FullPrefix возвращает полный namespace-префикс source:
// mountpoint + mount_as_namespace (если задано).
func (sc SourceConfig) FullPrefix() string {
	if sc.MountAsNamespace != "" {
		if sc.Mountpoint != "" {
			return sc.Mountpoint + "." + sc.MountAsNamespace
		}
		return sc.MountAsNamespace
	}
	return sc.Mountpoint
}

// DefaultConfigPath возвращает путь к конфигу по умолчанию.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "futi", "config.yaml"), nil
}

// DefaultModulesDir возвращает путь к каталогу модулей по умолчанию.
func DefaultModulesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "futi", "modules"), nil
}

// Load загружает конфиг из YAML-файла.
// Если файл не существует — возвращает nil, nil (fallback на default).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}

	for i, sc := range cfg.Sources {
		if sc.Path == "" {
			return nil, fmt.Errorf("config: source[%d]: path is required", i)
		}
		if sc.Mountpoint != "" {
			sc.Mountpoint = strings.Trim(sc.Mountpoint, ".")
			cfg.Sources[i].Mountpoint = sc.Mountpoint
		}
		if sc.MountAsNamespace != "" {
			sc.MountAsNamespace = strings.Trim(sc.MountAsNamespace, ".")
			cfg.Sources[i].MountAsNamespace = sc.MountAsNamespace
		}
	}

	return &cfg, nil
}
