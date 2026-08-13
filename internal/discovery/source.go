package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModuleInfo описывает обнаруженный модуль для `futi list`.
type ModuleInfo struct {
	Name         string
	ManifestPath string
	ModuleRoot   string
}

// Child описывает непосредственного ребёнка namespace: вложенный namespace или модуль.
type Child struct {
	Name         string // имя без namespace-префикса
	Type         string // "namespace" или "module"
	ManifestPath string // путь к манифесту (для module)
	ModuleRoot   string // корень модуля (для module)
}

// Source — источник модулей. Расширяемая точка для будущего
// многоисточникового разрешения (LocalSource, ProjectSource).
type Source interface {
	// Resolve находит модуль по namespace (например "dev.deploy").
	// Возвращает путь к манифесту и корень модуля (директория пакета).
	// Возвращает ошибку, если модуль не найден или обнаружен конфликт.
	Resolve(namespace string) (manifestPath, moduleRoot string, err error)

	// List возвращает все доступные модули.
	List() ([]ModuleInfo, error)

	// ListChildren возвращает непосредственных детей namespace:
	// вложенные namespaces и модули. Пустой namespace = корень.
	ListChildren(namespace string) ([]Child, error)
}

// GlobalSource ищет модули в фиксированном каталоге (например ~/.config/futi/modules/).
type GlobalSource struct {
	RootDir string
}

// NewGlobalSource создаёт GlobalSource с указанным корневым каталогом.
func NewGlobalSource(rootDir string) *GlobalSource {
	return &GlobalSource{RootDir: rootDir}
}

// Resolve реализует SaltStack-модель:
//   namespace "dev.deploy" → dev/deploy.yaml или dev/deploy/config.yaml
//   если найдены оба — ошибка конфликта
//   если ни один — ошибка not found
func (s *GlobalSource) Resolve(namespace string) (string, string, error) {
	segments := strings.Split(namespace, ".")
	relPath := filepath.Join(segments...)

	yamlPath := filepath.Join(s.RootDir, relPath+".yaml")
	pkgPath := filepath.Join(s.RootDir, relPath, "config.yaml")

	yamlExists := fileExists(yamlPath)
	pkgExists := fileExists(pkgPath)

	switch {
	case yamlExists && pkgExists:
		return "", "", fmt.Errorf("discovery: conflict for namespace %q: both %q and %q exist", namespace, yamlPath, pkgPath)
	case yamlExists:
		return yamlPath, filepath.Dir(yamlPath), nil
	case pkgExists:
		return pkgPath, filepath.Dir(pkgPath), nil
	default:
		return "", "", fmt.Errorf("discovery: module %q not found", namespace)
	}
}

// List обходит RootDir и находит все модули:
//   - *.yaml файлы на любом уровне вложенности
//   - */config.yaml (пакеты)
func (s *GlobalSource) List() ([]ModuleInfo, error) {
	// Non-existent root = no modules (fresh install)
	if _, err := os.Stat(s.RootDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("discovery: list: stat %q: %w", s.RootDir, err)
	}

	var modules []ModuleInfo

	err := filepath.WalkDir(s.RootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "config.yaml" && !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}

		rel, err := filepath.Rel(s.RootDir, path)
		if err != nil {
			return fmt.Errorf("discovery: list: %w", err)
		}

		var name string
		if d.Name() == "config.yaml" {
			name = strings.ReplaceAll(filepath.ToSlash(filepath.Dir(rel)), "/", ".")
		} else {
			name = strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(rel), ".yaml"), "/", ".")
		}

		modules = append(modules, ModuleInfo{
			Name:         name,
			ManifestPath: path,
			ModuleRoot:   filepath.Dir(path),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discovery: list: %w", err)
	}

	return modules, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// ListChildren возвращает непосредственных детей namespace:
//   - *.yaml файлы (кроме config.yaml) → child module
//   - поддиректории с config.yaml → child module (пакет)
//   - поддиректории без config.yaml → child namespace
// Пустой namespace = корень RootDir.
func (s *GlobalSource) ListChildren(namespace string) ([]Child, error) {
	var dir string
	if namespace == "" {
		dir = s.RootDir
	} else {
		segments := strings.Split(namespace, ".")
		dir = filepath.Join(s.RootDir, filepath.Join(segments...))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("discovery: namespace %q not found", namespace)
		}
		return nil, fmt.Errorf("discovery: list children %q: %w", namespace, err)
	}

	var children []Child
	for _, entry := range entries {
		if entry.IsDir() {
			childDir := filepath.Join(dir, entry.Name())
			configPath := filepath.Join(childDir, "config.yaml")
			if fileExists(configPath) {
				children = append(children, Child{
					Name:         entry.Name(),
					Type:         "module",
					ManifestPath: configPath,
					ModuleRoot:   childDir,
				})
			} else {
				children = append(children, Child{
					Name: entry.Name(),
					Type: "namespace",
				})
			}
		} else {
			name := entry.Name()
			if name == "config.yaml" || !strings.HasSuffix(name, ".yaml") {
				continue
			}
			childPath := filepath.Join(dir, name)
			children = append(children, Child{
				Name:         strings.TrimSuffix(name, ".yaml"),
				Type:         "module",
				ManifestPath: childPath,
				ModuleRoot:   dir,
			})
		}
	}

	return children, nil
}
