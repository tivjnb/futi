package discovery

import (
	"errors"
	"strings"
)

// FileSource — источник из одного YAML-файла модуля.
// Используется для авто-discovery: ./futi.yaml в CWD.
// Resolve сопоставляет namespace с именем модуля.
type FileSource struct {
	manifestPath string
	moduleRoot   string
	moduleName   string
}

// NewFileSource создаёт FileSource для одного файла модуля.
// moduleName — имя модуля (из поля name в YAML или имя файла).
func NewFileSource(manifestPath, moduleRoot, moduleName string) *FileSource {
	return &FileSource{
		manifestPath: manifestPath,
		moduleRoot:   moduleRoot,
		moduleName:   moduleName,
	}
}

func (s *FileSource) Resolve(namespace string) (string, string, error) {
	if namespace == s.moduleName {
		return s.manifestPath, s.moduleRoot, nil
	}
	return "", "", newModuleNotFound(namespace)
}

func (s *FileSource) List() ([]ModuleInfo, error) {
	return []ModuleInfo{
		{
			Name:         s.moduleName,
			ManifestPath: s.manifestPath,
			ModuleRoot:   s.moduleRoot,
		},
	}, nil
}

func (s *FileSource) ListChildren(namespace string) ([]Child, error) {
	if namespace == "" {
		return []Child{
			{
				Name:         s.moduleName,
				Type:         "module",
				ManifestPath: s.manifestPath,
				ModuleRoot:   s.moduleRoot,
			},
		}, nil
	}
	return nil, newNamespaceNotFound(namespace)
}

func (s *FileSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	return s.ListChildren(namespace)
}

// LayeredSource объединяет несколько источников с приоритетом.
// Первый источник (primary) имеет наивысший приоритет: если модуль
// найден в нескольких источниках, используется первичный.
// Используется для авто-discovery: локальный source перекрывает глобальный.
type LayeredSource struct {
	layers []Source
}

// NewLayeredSource создаёт LayeredSource из упорядоченного списка источников.
// Первый элемент — наивысший приоритет.
func NewLayeredSource(layers ...Source) *LayeredSource {
	return &LayeredSource{layers: layers}
}

func (s *LayeredSource) Resolve(namespace string) (string, string, error) {
	for _, layer := range s.layers {
		manifest, root, err := layer.Resolve(namespace)
		if err == nil {
			return manifest, root, nil
		}
		// Конфликт и I/O-ошибки не должны маскироваться модулем из нижнего слоя.
		if !errors.Is(err, ErrNotFound) {
			return "", "", err
		}
	}
	return "", "", newModuleNotFound(namespace)
}

func (s *LayeredSource) List() ([]ModuleInfo, error) {
	seen := make(map[string]bool)
	var modules []ModuleInfo
	for _, layer := range s.layers {
		layerModules, err := layer.List()
		if err != nil {
			continue
		}
		for _, m := range layerModules {
			if seen[m.Name] {
				continue
			}
			seen[m.Name] = true
			modules = append(modules, m)
		}
	}
	return modules, nil
}

// ListChildren объединяет детей namespace из всех слоёв. Ключ слияния — пара
// (имя, тип): модуль x и namespace x сосуществуют, как и в Resolve
// (x — модуль, x.y — внутри namespace). Внутри одного ключа побеждает
// верхний слой; запись берётся целиком, вместе с пометкой Conflict.
func (s *LayeredSource) ListChildren(namespace string) ([]Child, error) {
	type childKey struct{ name, typ string }
	seen := make(map[childKey]bool)
	var result []Child
	var anyErr error
	foundAny := false

	for _, layer := range s.layers {
		children, err := layer.ListChildren(namespace)
		if err != nil {
			anyErr = err
			continue
		}
		foundAny = true
		for _, c := range children {
			key := childKey{c.Name, c.Type}
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, c)
		}
	}

	if !foundAny && anyErr != nil {
		return nil, anyErr
	}
	return result, nil
}

// ListChildrenRecursive делегирует обход поддерева каждому слою (защита от
// циклов симлинков — в самих слоях) и сливает результаты как ListChildren:
// ключ — пара (имя, тип), побеждает верхний слой, Conflict сохраняется.
// Слой, в котором namespace нет (ErrNotFound), пропускается, если namespace
// листит другой слой; если не листит никто — ошибка «не найден». Остальные
// ошибки (конфликт модуля из двух sources в Composite на любом уровне,
// invalid namespace, ввод-вывод) пробрасываются всегда: иначе пропал бы
// весь слой, а ошибка осталась бы незамеченной.
func (s *LayeredSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	var lists [][]Child
	var anyErr error
	foundAny := false

	for _, layer := range s.layers {
		children, err := layer.ListChildrenRecursive(namespace)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				return nil, err
			}
			anyErr = err
			continue
		}
		foundAny = true
		lists = append(lists, children)
	}

	if !foundAny && anyErr != nil {
		return nil, anyErr
	}
	return mergeRecursive(lists), nil
}

// mergeRecursive сливает рекурсивные листинги (имена — пути от общего
// контекста) в один. Ключ — пара (имя, тип), побеждает запись из более
// раннего списка. Порядок — как при рекурсии через ListChildren по
// объединённому дереву: обход в глубину, дети каждого namespace — в порядке
// списков, внутри списка — в его порядке. Предусловие обхода — родитель-
// namespace записи присутствует в объединённых списках. Записи без него
// (модуль `my.tool` из ./futi.yaml при отсутствии namespace `my`) обход не
// посещает — они добавляются после него в исходном порядке.
func mergeRecursive(lists [][]Child) []Child {
	type childKey struct{ name, typ string }
	seen := make(map[childKey]bool)
	var merged []Child
	byParent := make(map[string][]Child)
	for _, list := range lists {
		for _, c := range list {
			key := childKey{c.Name, c.Type}
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, c)
			parent := ""
			if i := strings.LastIndex(c.Name, "."); i >= 0 {
				parent = c.Name[:i]
			}
			byParent[parent] = append(byParent[parent], c)
		}
	}

	var result []Child
	visited := make(map[childKey]bool)
	var walk func(parent string)
	walk = func(parent string) {
		for _, c := range byParent[parent] {
			result = append(result, c)
			visited[childKey{c.Name, c.Type}] = true
			if c.Type == "namespace" {
				walk(c.Name)
			}
		}
	}
	walk("")
	for _, c := range merged {
		if !visited[childKey{c.Name, c.Type}] {
			result = append(result, c)
		}
	}
	return result
}
