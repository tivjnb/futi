// Package metayaml читает и пишет sidecar-файл __futi_meta.yaml — маркер
// «модуль установлен через futi modules install».
//
// Два раздела (спека MODULE_MANAGMENT.md §4):
//   - lockfile — авто-данные установки (source/type/ref/commit/hash/даты);
//     перезаписываются при каждом install/update;
//   - about — сведения от автора модуля; сохраняются из источника.
package metayaml

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileName — имя sidecar-файла (кладётся рядом с config.yaml).
const FileName = "__futi_meta.yaml"

// ExcludedDirs — каталоги, исключаемые из хеша дерева и из копирования
// при install из локального каталога: артефакты сборки и VCS.
var ExcludedDirs = []string{".venv", "__pycache__", ".git", "node_modules"}

// Lockfile — авто-раздел: источник и состояние установки.
type Lockfile struct {
	Source      string    `yaml:"source"`           // путь/URL источника
	Type        string    `yaml:"type"`             // local | git | archive
	Ref         string    `yaml:"ref,omitempty"`    // branch/tag (git), версия (archive)
	Commit      string    `yaml:"commit,omitempty"` // full SHA (только git)
	Hash        string    `yaml:"hash"`             // sha256:<hex> дерева модуля
	InstalledAt time.Time `yaml:"installed_at"`
	LastCheck   time.Time `yaml:"last_check"`
}

// About — раздел автора модуля. Сохраняется из источника при install/update.
// Новое поле — добавить в isEmptyAbout.
type About struct {
	Author   string   `yaml:"author,omitempty"`
	License  string   `yaml:"license,omitempty"`
	Homepage string   `yaml:"homepage,omitempty"`
	Tags     []string `yaml:"tags,omitempty"`
}

// Meta — полное содержимое __futi_meta.yaml.
type Meta struct {
	Lockfile Lockfile `yaml:"lockfile"`
	About    *About   `yaml:"about,omitempty"`
}

// osReadFile — var для подмены в тестах (паттерн envMapFn в execmodule).
var osReadFile = os.ReadFile

// Read читает и парсит __futi_meta.yaml по пути path.
func Read(path string) (*Meta, error) {
	data, err := osReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("metayaml: read %q: %w", path, err)
	}
	// Документ без содержимого (0 байт, пробелы, только комментарии) — не
	// валидный sidecar: yaml.Unmarshal принял бы его как нулевой Meta.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("metayaml: parse %q: %w", path, err)
	}
	if doc.Kind == 0 || (len(doc.Content) == 1 && doc.Content[0].Tag == "!!null") {
		return nil, fmt.Errorf("metayaml: %q is empty", path)
	}
	var m Meta
	if err := doc.Decode(&m); err != nil {
		return nil, fmt.Errorf("metayaml: parse %q: %w", path, err)
	}
	return &m, nil
}

// Write записывает Meta в path: temp + rename. Атомарна замена файла (при
// краше не остаётся частичной записи); долговечность — fsync файла до
// rename и fsync каталога после (best-effort). Файл получает права 0644
// (CreateTemp даёт 0600). Если path — симлинк, rename заменяет сам симлинк,
// а не его цель: запись в цель вне каталога модуля опаснее.
func Write(path string, m *Meta) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("metayaml: marshal: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+FileName+"-*")
	if err != nil {
		return fmt.Errorf("metayaml: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op после успешного rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("metayaml: write temp: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("metayaml: chmod temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("metayaml: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("metayaml: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("metayaml: rename: %w", err)
	}
	// fsync каталога фиксирует rename; best-effort: на Windows каталог для
	// Sync не открыть, ошибки игнорируем.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// MergeAbout возвращает Meta с lockfile из dest и about из source
// (если у source есть непустой About; иначе about из dest). Правила — спека §4.1.
func MergeAbout(dest, source *Meta) *Meta {
	merged := &Meta{Lockfile: dest.Lockfile, About: dest.About}
	if source != nil && source.About != nil && !isEmptyAbout(source.About) {
		merged.About = source.About
	}
	return merged
}

func isEmptyAbout(a *About) bool {
	return a.Author == "" && a.License == "" && a.Homepage == "" && len(a.Tags) == 0
}

// IsExcluded сообщает, содержит ли относительный путь (слэши) исключённый
// компонент. Используется и хешем дерева, и сканированием installed-модулей.
func IsExcluded(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range parts {
		for _, ex := range ExcludedDirs {
			if p == ex {
				return true
			}
		}
	}
	return false
}

// ComputeTreeHash считает SHA256 дерева модуля: конкатенация строк
// "relpath\x00sha256(content)\n" по всем regular-файлам, отсортированным
// лексикографически. Исключения: __futi_meta.yaml и ExcludedDirs-компоненты.
// Возвращает "sha256:<hex>".
func ComputeTreeHash(root string) (string, error) {
	var lines []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if IsExcluded(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // симлинки и прочее не хешируем
		}
		if rel == FileName || IsExcluded(rel) {
			return nil
		}
		data, err := osReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, fmt.Sprintf("%s\x00%s\n", filepath.ToSlash(rel), hex.EncodeToString(sum[:])))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("metayaml: hash tree %q: %w", root, err)
	}
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "")))
	return "sha256:" + hex.EncodeToString(h[:]), nil
}
