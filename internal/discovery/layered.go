package discovery

import (
	"fmt"
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
	return "", "", fmt.Errorf("discovery: module %q not found", namespace)
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
	return nil, fmt.Errorf("discovery: namespace %q not found", namespace)
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
	}
	return "", "", fmt.Errorf("discovery: module %q not found", namespace)
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

func (s *LayeredSource) ListChildren(namespace string) ([]Child, error) {
	seen := make(map[string]bool)
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
			if seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			result = append(result, c)
		}
	}

	if !foundAny && anyErr != nil {
		return nil, anyErr
	}
	return result, nil
}

func (s *LayeredSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	children, err := s.ListChildren(namespace)
	if err != nil {
		return nil, err
	}

	var result []Child
	for _, c := range children {
		result = append(result, c)
		if c.Type == "namespace" {
			fullNs := c.Name
			if namespace != "" {
				fullNs = namespace + "." + c.Name
			}
			subChildren, err := s.ListChildrenRecursive(fullNs)
			if err != nil {
				return nil, err
			}
			for _, sc := range subChildren {
				sc.Name = c.Name + "." + sc.Name
				result = append(result, sc)
			}
		}
	}

	return result, nil
}
