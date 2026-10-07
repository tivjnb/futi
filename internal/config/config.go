package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"github.com/tivjnb/futi/internal/yamlstrict"
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

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}
	if errs := yamlstrict.UnknownKeys(&node, reflect.TypeOf(Config{})); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return nil, fmt.Errorf("config: %q: %s", path, strings.Join(msgs, "; "))
	}
	// Пустой документ (Kind == 0) не декодируем: получится пустой Config.
	var cfg Config
	if node.Kind != 0 {
		if err := node.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("config: parse %q: %w", path, err)
		}
	}

	absCfgPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config: abs path %q: %w", path, err)
	}
	cfgDir := filepath.Dir(absCfgPath)

	for i, sc := range cfg.Sources {
		if sc.Path == "" {
			return nil, fmt.Errorf("config: source[%d]: path is required", i)
		}
		resolved, err := resolveSourcePath(sc.Path, cfgDir)
		if err != nil {
			return nil, fmt.Errorf("config: source[%d]: %w", i, err)
		}
		cfg.Sources[i].Path = resolved
		mp, err := normalizeNamespace(sc.Mountpoint)
		if err != nil {
			return nil, fmt.Errorf("config: source[%d]: invalid mountpoint %q: %w", i, sc.Mountpoint, err)
		}
		cfg.Sources[i].Mountpoint = mp
		ns, err := normalizeNamespace(sc.MountAsNamespace)
		if err != nil {
			return nil, fmt.Errorf("config: source[%d]: invalid mount_as_namespace %q: %w", i, sc.MountAsNamespace, err)
		}
		cfg.Sources[i].MountAsNamespace = ns
	}

	return &cfg, nil
}

// normalizeNamespace обрезает внешние точки и проверяет результат: непустое
// значение не должно превратиться в пустое (".", ".."), а каждый сегмент —
// быть непустым и без разделителей путей, пробельных и управляющих символов.
// Иначе источник молча смонтировался бы в корень или в недостижимый namespace.
func normalizeNamespace(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	trimmed := strings.Trim(raw, ".")
	if trimmed == "" {
		return "", fmt.Errorf("no namespace name left after trimming dots")
	}
	for _, seg := range strings.Split(trimmed, ".") {
		if seg == "" {
			return "", fmt.Errorf("empty namespace segment")
		}
		for _, r := range seg {
			if r == '/' || r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
				return "", fmt.Errorf("segment %q contains forbidden character %q", seg, r)
			}
		}
	}
	return trimmed, nil
}

// resolveSourcePath приводит path источника к очищенному абсолютному виду:
// "~" и "~/…" — от домашнего каталога, относительный путь — от каталога
// config.yaml (а не от cwd), абсолютный — без изменений. "~имя/…" — ошибка:
// иначе опечатка молча превратилась бы в пустой источник внутри каталога конфига.
func resolveSourcePath(p, cfgDir string) (string, error) {
	if strings.HasPrefix(p, "~") {
		rest := p[1:]
		if rest != "" && rest[0] != '/' && rest[0] != filepath.Separator {
			return "", fmt.Errorf("path %q: ~user is not supported, use ~/… or an absolute path", p)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("path %q: get home dir: %w", p, err)
		}
		return filepath.Join(home, rest), nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Join(cfgDir, p), nil
}
