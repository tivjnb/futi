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
	Conflict     bool   // рядом лежат x.yaml и x/config.yaml: Resolve вернёт ошибку конфликта
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
// Записи, которые Resolve не найдёт, пропускаются (см. classifyEntry):
// битые симлинки, FIFO и устройства с именем *.yaml, имена с точкой,
// пустые и "config".
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

	entries, err := readModuleEntries(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.kind == entryNamespace {
			s.walkModules(e.path, nsPrefix+e.name+".", modules, visited)
			continue
		}
		*modules = append(*modules, ModuleInfo{
			Name:         nsName(nsPrefix, e.name),
			ManifestPath: e.manifestPath,
			ModuleRoot:   e.moduleRoot,
		})
	}
}

// entryKind — вид записи каталога модулей.
type entryKind int

const (
	entryNamespace  entryKind = iota + 1 // каталог без config.yaml
	entryPackage                         // каталог с config.yaml — модуль-пакет
	entryYAMLModule                      // обычный файл <name>.yaml — модуль
)

// moduleEntry — запись каталога модулей, которую найдёт Resolve.
type moduleEntry struct {
	kind         entryKind
	name         string // сегмент namespace: имя каталога или файла без .yaml
	path         string // путь записи: каталог или .yaml-файл
	manifestPath string // манифест (для пакета и yaml-модуля)
	moduleRoot   string // корень модуля (для пакета и yaml-модуля)
	conflict     bool   // x.yaml и x/config.yaml рядом: записи сведены в одну
}

// readModuleEntries читает dir и классифицирует записи (classifyEntry);
// пропущенные записи не возвращаются. Порядок — как у os.ReadDir (по имени).
// Конфликт x.yaml и x/config.yaml (Resolve вернёт для него ошибку) сводится
// в одну запись yaml-модуля с conflict=true: манифест от x.yaml,
// детерминированно; запись-пакет отбрасывается.
// Общий шаг всех обходов GlobalSource: листинги согласованы между собой
// и с Resolve.
func readModuleEntries(dir string) ([]moduleEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var entries []moduleEntry
	for _, de := range dirEntries {
		if e, ok := classifyEntry(dir, de); ok {
			entries = append(entries, e)
		}
	}
	return mergeConflicts(entries), nil
}

// mergeConflicts сводит пары «yaml-модуль x + пакет x» в одну запись
// yaml-модуля с conflict=true. Остальные записи и их порядок не меняются.
func mergeConflicts(entries []moduleEntry) []moduleEntry {
	packages := make(map[string]bool)
	for _, e := range entries {
		if e.kind == entryPackage {
			packages[e.name] = true
		}
	}
	yamlWithPackage := make(map[string]bool)
	merged := make([]moduleEntry, 0, len(entries))
	for _, e := range entries {
		if e.kind == entryYAMLModule && packages[e.name] {
			e.conflict = true
			yamlWithPackage[e.name] = true
		}
		merged = append(merged, e)
	}
	result := merged[:0]
	for _, e := range merged {
		if e.kind == entryPackage && yamlWithPackage[e.name] {
			continue
		}
		result = append(result, e)
	}
	return result
}

// classifyEntry определяет вид записи de каталога dir, следуя симлинкам.
// ok=false — запись пропускается, потому что Resolve её не найдёт:
//   - сегмент namespace пустой, содержит "." или равен "config"
//     (my.tool.yaml, .yaml, foo..yaml, config.yaml, my.dir/, .git/, config/);
//   - .yaml-файл (в т.ч. симлинк), у которого Stat падает или который не
//     обычный файл (битый симлинк, FIFO, устройство);
//   - не каталог и не .yaml-файл.
func classifyEntry(dir string, de os.DirEntry) (moduleEntry, bool) {
	path := filepath.Join(dir, de.Name())
	if entryIsDir(path, de) {
		if !validSegment(de.Name()) {
			return moduleEntry{}, false
		}
		configPath := filepath.Join(path, "config.yaml")
		if fileExists(configPath) {
			return moduleEntry{kind: entryPackage, name: de.Name(), path: path, manifestPath: configPath, moduleRoot: path}, true
		}
		return moduleEntry{kind: entryNamespace, name: de.Name(), path: path}, true
	}
	name, isYAML := strings.CutSuffix(de.Name(), ".yaml")
	if !isYAML || !validSegment(name) || !fileExists(path) {
		return moduleEntry{}, false
	}
	return moduleEntry{kind: entryYAMLModule, name: name, path: path, manifestPath: path, moduleRoot: dir}, true
}

// validSegment сообщает, что name годится как сегмент namespace для Resolve:
// непустой, без "." (Resolve делит namespace по точкам) и не "config"
// (зарезервирован за манифестом пакета).
func validSegment(name string) bool {
	return name != "" && !strings.Contains(name, ".") && name != "config"
}

// entryIsDir определяет, является ли entry директорией, следуя симлинкам.
// Битый симлинк (цель недоступна) — не директория; как файл он тоже не
// листится (classifyEntry: Stat падает).
func entryIsDir(path string, entry os.DirEntry) bool {
	if entry.Type()&os.ModeSymlink == 0 {
		return entry.IsDir()
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
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

// statFile сообщает, что path — существующий обычный файл (не каталог, FIFO,
// устройство или сокет).
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
	return info.Mode().IsRegular(), nil
}

// fileExists сообщает, что path — существующий обычный файл.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// namespaceDir возвращает каталог namespace ("" — RootDir). Сегменты
// проверяются по правилам Resolve: пустой сегмент ("a..b", ".git", "a.") —
// ошибка invalid namespace (filepath.Join схлопнул бы его в другой каталог),
// зарезервированный "config" — namespace не найден.
func (s *GlobalSource) namespaceDir(namespace string) (string, error) {
	if namespace == "" {
		return s.RootDir, nil
	}
	segments := strings.Split(namespace, ".")
	for _, seg := range segments {
		if seg == "" {
			return "", fmt.Errorf("discovery: invalid namespace %q: empty segment", namespace)
		}
		if !validSegment(seg) {
			return "", fmt.Errorf("discovery: namespace %q not found", namespace)
		}
	}
	return filepath.Join(s.RootDir, filepath.Join(segments...)), nil
}

// ListChildren возвращает непосредственных детей namespace:
//   - *.yaml файлы (кроме config.yaml) → child module
//   - поддиректории с config.yaml → child module (пакет)
//   - поддиректории без config.yaml → child namespace
// Записи, которые Resolve не найдёт, пропускаются (см. classifyEntry).
// Пустой namespace = корень RootDir.
func (s *GlobalSource) ListChildren(namespace string) ([]Child, error) {
	dir, err := s.namespaceDir(namespace)
	if err != nil {
		return nil, err
	}

	entries, err := readModuleEntries(dir)
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
	for _, e := range entries {
		if e.kind == entryNamespace {
			children = append(children, Child{Name: e.name, Type: "namespace"})
			continue
		}
		children = append(children, Child{
			Name:         e.name,
			Type:         "module",
			ManifestPath: e.manifestPath,
			ModuleRoot:   e.moduleRoot,
			Conflict:     e.conflict,
		})
	}

	return children, nil
}

// ListChildrenRecursive рекурсивно обходит поддерево namespace
// и возвращает всех потомков. Name каждого Child — полный namespace-путь
// от контекста (например для контекста "images": "png", "converter",
// "converter.to_png").
func (s *GlobalSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	dir, err := s.namespaceDir(namespace)
	if err != nil {
		return nil, err
	}
	// Проверяем существование namespace
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

	entries, err := readModuleEntries(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		fullName := e.name
		if prefix != "" {
			fullName = prefix + "." + e.name
		}
		if e.kind == entryNamespace {
			*result = append(*result, Child{Name: fullName, Type: "namespace"})
			s.walkChildrenRecursive(e.path, fullName, result, visited)
			continue
		}
		// Модуль (в т.ч. пакет — лист: внутрь не углубляемся).
		*result = append(*result, Child{
			Name:         fullName,
			Type:         "module",
			ManifestPath: e.manifestPath,
			ModuleRoot:   e.moduleRoot,
			Conflict:     e.conflict,
		})
	}
}
