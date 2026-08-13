package discovery

import (
	"fmt"
	"strings"

	"futi/internal/config"
)

// CompositeSource агрегирует несколько GlobalSource с namespace-маппингом.
// Реализует интерфейс Source, делегируя вызовы в соответствующие GlobalSource
// с трансляцией namespace.
type CompositeSource struct {
	configs []config.SourceConfig
	sources []*GlobalSource
}

// NewCompositeSource создаёт CompositeSource из конфигурации sources.
func NewCompositeSource(cfg *config.Config) *CompositeSource {
	cs := &CompositeSource{}
	if cfg == nil {
		return cs
	}
	for _, sc := range cfg.Sources {
		cs.configs = append(cs.configs, sc)
		cs.sources = append(cs.sources, NewGlobalSource(sc.Path))
	}
	return cs
}

// Resolve находит модуль по полному namespace, проверяя все sources.
// Если модуль найден в нескольких sources — ошибка конфликта.
func (cs *CompositeSource) Resolve(namespace string) (string, string, error) {
	var foundManifest, foundRoot string
	foundCount := 0

	for i, sc := range cs.configs {
		prefix := sc.FullPrefix()

		var relNs string
		if prefix == "" {
			relNs = namespace
		} else if namespace == prefix {
			// namespace == prefix: это mountpoint (namespace, не module)
			continue
		} else if strings.HasPrefix(namespace, prefix+".") {
			relNs = namespace[len(prefix)+1:]
		} else {
			continue
		}

		manifest, root, err := cs.sources[i].Resolve(relNs)
		if err == nil {
			foundCount++
			foundManifest = manifest
			foundRoot = root
		}
	}

	switch foundCount {
	case 0:
		return "", "", fmt.Errorf("discovery: module %q not found", namespace)
	case 1:
		return foundManifest, foundRoot, nil
	default:
		return "", "", fmt.Errorf("discovery: conflict for namespace %q: found in %d sources", namespace, foundCount)
	}
}

// ListChildren возвращает непосредственных детей namespace, объединяя
// результаты из всех sources. Если два source дают модуль с одинаковым
// именем — ошибка конфликта.
func (cs *CompositeSource) ListChildren(namespace string) ([]Child, error) {
	type childEntry struct {
		typ           string
		moduleCount   int
	}
	seen := make(map[string]*childEntry)
	var result []Child

	addNamespace := func(name string) {
		if e, ok := seen[name]; ok {
			if e.typ == "module" {
				// Conflict: name is both module and namespace
				return // will be reported as error below
			}
			return // already seen as namespace
		}
		seen[name] = &childEntry{typ: "namespace"}
		result = append(result, Child{Name: name, Type: "namespace"})
	}

	addModule := func(c Child) {
		if e, ok := seen[c.Name]; ok {
			if e.typ == "namespace" {
				return // conflict: will be detected
			}
			e.moduleCount++
			if e.moduleCount > 1 {
				return // conflict: will be reported
			}
			return // already have this module from another source
		}
		seen[c.Name] = &childEntry{typ: "module", moduleCount: 1}
		result = append(result, c)
	}

	for i, sc := range cs.configs {
		prefix := sc.FullPrefix()

		switch {
		case prefix == namespace:
			// Source смонтирован ровно тут — его прямые дети
			children, err := cs.sources[i].ListChildren("")
			if err != nil {
				continue
			}
			for _, c := range children {
				if c.Type == "module" {
					addModule(c)
				} else {
					addNamespace(c.Name)
				}
			}

		case namespace == "" && prefix != "":
			// Листинг корня, source не в корне — первый сегмент = child namespace
			firstSeg := strings.SplitN(prefix, ".", 2)[0]
			addNamespace(firstSeg)

		case strings.HasPrefix(prefix, namespace+"."):
			// Source смонтирован ниже — следующий сегмент = child namespace
			rest := prefix[len(namespace)+1:]
			firstSeg := strings.SplitN(rest, ".", 2)[0]
			addNamespace(firstSeg)

		case prefix == "" || strings.HasPrefix(namespace, prefix+"."):
			// Source содержит namespace в своём дереве
			relNs := namespace
			if prefix != "" {
				relNs = namespace[len(prefix)+1:]
			}
			children, err := cs.sources[i].ListChildren(relNs)
			if err != nil {
				continue
			}
			for _, c := range children {
				if c.Type == "module" {
					addModule(c)
				} else {
					addNamespace(c.Name)
				}
			}
		}
	}

	// Проверка конфликтов
	for name, e := range seen {
		if e.moduleCount > 1 {
			return nil, fmt.Errorf("discovery: conflict: module %q found in multiple sources", name)
		}
		if e.typ == "module" {
			// Check if it's also a namespace (shouldn't happen, but check)
			for _, c := range result {
				if c.Name == name && c.Type != "module" {
					return nil, fmt.Errorf("discovery: conflict: %q is both module and namespace", name)
				}
			}
		}
	}

	return result, nil
}

// ListChildrenRecursive рекурсивно обходит поддерево namespace,
// объединяя результаты из всех sources.
func (cs *CompositeSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	children, err := cs.ListChildren(namespace)
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
			subChildren, err := cs.ListChildrenRecursive(fullNs)
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

// List возвращает все модули из всех sources.
// Примечание: в CLI сейчас используется ListChildren/ListChildrenRecursive;
// List оставлен для обратной совместимости и тестов.
func (cs *CompositeSource) List() ([]ModuleInfo, error) {
	var modules []ModuleInfo
	for i, sc := range cs.configs {
		srcModules, err := cs.sources[i].List()
		if err != nil {
			continue
		}
		prefix := sc.FullPrefix()
		for _, m := range srcModules {
			if prefix != "" {
				m.Name = prefix + "." + m.Name
			}
			modules = append(modules, m)
		}
	}
	return modules, nil
}
