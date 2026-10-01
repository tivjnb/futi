package discovery

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// Source — источник модулей. Реализации: GlobalSource (один каталог),
// CompositeSource (несколько каталогов с namespace-маппингом).
// Расширяемая точка для будущих источников (LocalSource, ProjectSource).
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

	// ListChildrenRecursive рекурсивно обходит поддерево namespace
	// и возвращает всех потомков с полным namespace-путём от контекста.
	// Пустой namespace = корень.
	ListChildrenRecursive(namespace string) ([]Child, error)
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
// Пустой namespace и пустые сегменты ("a..b", ".a", "a.") — ошибка:
// иначе пустой путь резолвился бы в корневой config.yaml.
// Пакет — лист: если любой родительский каталог пути содержит config.yaml,
// модуль «внутри пакета» не существует; сегмент "config" зарезервирован
// (иначе dev.config резолвился бы в манифест пакета dev/config.yaml).
func (s *GlobalSource) Resolve(namespace string) (string, string, error) {
	segments := strings.Split(namespace, ".")
	for _, seg := range segments {
		if seg == "" {
			return "", "", fmt.Errorf("discovery: invalid namespace %q: empty segment", namespace)
		}
	}
	for _, seg := range segments {
		if seg == "config" {
			return "", "", newModuleNotFound(namespace)
		}
	}
	insidePackage, err := s.withinPackage(segments[:len(segments)-1])
	if err != nil {
		return "", "", err
	}
	if insidePackage {
		return "", "", newModuleNotFound(namespace)
	}
	relPath := filepath.Join(segments...)

	yamlPath := filepath.Join(s.RootDir, relPath+".yaml")
	pkgPath := filepath.Join(s.RootDir, relPath, "config.yaml")

	yamlExists, err := statFile(yamlPath)
	if err != nil {
		return "", "", fmt.Errorf("discovery: stat %q: %w", yamlPath, err)
	}
	pkgExists, err := statFile(pkgPath)
	if err != nil {
		return "", "", fmt.Errorf("discovery: stat %q: %w", pkgPath, err)
	}

	switch {
	case yamlExists && pkgExists:
		return "", "", fmt.Errorf("discovery: conflict for namespace %q: both %q and %q exist", namespace, yamlPath, pkgPath)
	case yamlExists:
		return yamlPath, filepath.Dir(yamlPath), nil
	case pkgExists:
		return pkgPath, filepath.Dir(pkgPath), nil
	default:
		return "", "", newModuleNotFound(namespace)
	}
}

// withinPackage сообщает, что путь segments — пакет или лежит внутри пакета:
// какой-либо из каталогов segments[:1], segments[:2], …, segments содержит
// config.yaml. Пакет — лист: ни модулей, ни namespace внутри него нет.
// Ошибка stat (кроме отсутствия) возвращается: её нельзя считать «не пакет».
func (s *GlobalSource) withinPackage(segments []string) (bool, error) {
	for i := 1; i <= len(segments); i++ {
		configPath := filepath.Join(s.RootDir, filepath.Join(segments[:i]...), "config.yaml")
		isPackage, err := statFile(configPath)
		if err != nil {
			return false, fmt.Errorf("discovery: stat %q: %w", configPath, err)
		}
		if isPackage {
			return true, nil
		}
	}
	return false, nil
}

// List обходит RootDir и находит все модули:
//   - *.yaml файлы на любом уровне вложенности
//   - */config.yaml (пакеты)
//
// Симлинки поддерживаются (dotfiles-менеджеры, dev-симлинки из репо):
// симлинк на .yaml-файл и симлинк на директорию (namespace или пакет)
// обрабатываются так же, как реальные файлы/директории. Циклы симлинков
// (a → b → a) и повторный обход алиасов отсекаются по резолвнутому пути.
// Битые симлинки (цель недоступна) пропускаются.
func (s *GlobalSource) List() ([]ModuleInfo, error) {
	// Non-existent root = no modules (fresh install)
	if _, err := os.Stat(s.RootDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("discovery: list: stat %q: %w", s.RootDir, err)
	}

	var modules []ModuleInfo
	s.walkModules(s.RootDir, "", &modules, make(map[string]bool))
	return modules, nil
}

// walkModules рекурсивно обходит dir и собирает модули в *modules.
// nsPrefix — namespace-префикс ("" для корня). visited — множество
// резолвнутых (EvalSymlinks) путей уже обойдённых директорий: защита от
// циклов симлинков и повторного обхода алиасов на один каталог.
// Пакет (директория с config.yaml) — лист: внутрь не заходим, его *.yaml
// не модули (согласовано с Resolve и ListChildrenRecursive).
func (s *GlobalSource) walkModules(dir, nsPrefix string, modules *[]ModuleInfo, visited map[string]bool) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return // недоступная директория — пропускаем
	}
	if visited[real] {
		return
	}
	visited[real] = true

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entryIsDir(path, entry) {
			// config.yaml внутри → директория (возможно, симлинк) — пакет.
			if fileExists(filepath.Join(path, "config.yaml")) {
				*modules = append(*modules, ModuleInfo{
					Name:         nsName(nsPrefix, entry.Name()),
					ManifestPath: filepath.Join(path, "config.yaml"),
					ModuleRoot:   path,
				})
				continue
			}
			s.walkModules(path, nsPrefix+entry.Name()+".", modules, visited)
			continue
		}
		name := entry.Name()
		if name == "config.yaml" || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		*modules = append(*modules, ModuleInfo{
			Name:         nsName(nsPrefix, strings.TrimSuffix(name, ".yaml")),
			ManifestPath: path,
			ModuleRoot:   filepath.Dir(path),
		})
	}
}

// entryIsDir определяет, является ли entry директорией, следуя симлинкам.
// Битый симлинк (цель недоступна) трактуется как файл: если имя *.yaml,
// он остаётся в listing'е как модуль — цель может стать доступной позже
// (кэш сохраняет запись при временной ошибке Stat).
func entryIsDir(path string, entry os.DirEntry) bool {
	if entry.Type()&os.ModeSymlink == 0 {
		return entry.IsDir()
	}
	info, err := os.Stat(path)
	if err != nil {
		return false // битый симлинк — трактуем как файл
	}
	return info.IsDir()
}

// nsName строит имя модуля с namespace-префиксом.
func nsName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + name
}

// statFile сообщает, что path — существующий файл (не каталог).
// Отсутствие пути (в том числе когда родитель — не каталог) и каталог — это
// false без ошибки; любая другая ошибка stat (например, EACCES) возвращается
// вызывающему: её нельзя трактовать как «модуля нет».
func statFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
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
			if namespace == "" {
				return nil, nil
			}
			return nil, fmt.Errorf("discovery: namespace %q not found", namespace)
		}
		return nil, fmt.Errorf("discovery: list children %q: %w", namespace, err)
	}
	if namespace != "" {
		insidePackage, err := s.withinPackage(strings.Split(namespace, "."))
		if err != nil {
			return nil, err
		}
		if insidePackage {
			return nil, nil // пакет — лист: детей нет, как и для Resolve
		}
	}

	var children []Child
	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if entryIsDir(entryPath, entry) {
			childDir := entryPath
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

// ListChildrenRecursive рекурсивно обходит поддерево namespace
// и возвращает всех потомков. Name каждого Child — полный namespace-путь
// от контекста (например для контекста "images": "png", "converter",
// "converter.to_png").
func (s *GlobalSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	// Проверяем существование namespace
	var dir string
	if namespace == "" {
		dir = s.RootDir
	} else {
		segments := strings.Split(namespace, ".")
		dir = filepath.Join(s.RootDir, filepath.Join(segments...))
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			if namespace == "" {
				return nil, nil
			}
			return nil, fmt.Errorf("discovery: namespace %q not found", namespace)
		}
		return nil, fmt.Errorf("discovery: list recursive %q: %w", namespace, err)
	}
	if namespace != "" {
		insidePackage, err := s.withinPackage(strings.Split(namespace, "."))
		if err != nil {
			return nil, err
		}
		if insidePackage {
			return nil, nil // пакет — лист: потомков нет, как и для Resolve
		}
	}

	var result []Child
	s.walkChildrenRecursive(dir, "", &result, make(map[string]bool))
	return result, nil
}

// walkChildrenRecursive обходит директорию dir. prefix — префикс для
// детей текущего уровня: при "" дети не получают префикса, иначе
// fullName = prefix + "." + childName.
// visited — множество резолвнутых путей уже обойдённых директорий:
// защита от циклов симлинков (a → b → a) — обход каждого реального
// каталога выполняется один раз (первым встреченным путём).
func (s *GlobalSource) walkChildrenRecursive(dir, prefix string, result *[]Child, visited map[string]bool) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return // недоступная директория — пропускаем
	}
	if visited[real] {
		return
	}
	visited[real] = true

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if !entryIsDir(entryPath, entry) {
			name := entry.Name()
			if name == "config.yaml" || !strings.HasSuffix(name, ".yaml") {
				continue
			}
			childPath := entryPath
			childName := strings.TrimSuffix(name, ".yaml")
			fullName := childName
			if prefix != "" {
				fullName = prefix + "." + childName
			}
			*result = append(*result, Child{
				Name:         fullName,
				Type:         "module",
				ManifestPath: childPath,
				ModuleRoot:   dir,
			})
		} else {
			childDir := entryPath
			configPath := filepath.Join(childDir, "config.yaml")
			childName := entry.Name()
			fullName := childName
			if prefix != "" {
				fullName = prefix + "." + childName
			}
			if fileExists(configPath) {
				*result = append(*result, Child{
					Name:         fullName,
					Type:         "module",
					ManifestPath: configPath,
					ModuleRoot:   childDir,
				})
				// Модуль-пакет: не углубляемся внутрь
			} else {
				*result = append(*result, Child{
					Name: fullName,
					Type: "namespace",
				})
				s.walkChildrenRecursive(childDir, fullName, result, visited)
			}
		}
	}
}
