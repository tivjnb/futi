package discovery

import (
	"errors"
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
		} else if !errors.Is(err, ErrNotFound) {
			// Конфликт/I/O-ошибка источника — не «не найдено», не считаем как отсутствие.
			return "", "", err
		}
	}

	switch foundCount {
	case 0:
		return "", "", newModuleNotFound(namespace)
	case 1:
		return foundManifest, foundRoot, nil
	default:
		return "", "", fmt.Errorf("discovery: conflict for namespace %q: found in %d sources", namespace, foundCount)
	}
}

// ListChildren возвращает непосредственных детей namespace, объединяя
// результаты из всех sources. Ключ слияния — пара (имя, тип): модуль x и
// namespace x сосуществуют, как и в Resolve (x — модуль, x.y — внутри
// namespace). Если два source дают модуль с одинаковым именем — ошибка
// конфликта (Resolve для него тоже вернёт конфликт).
func (cs *CompositeSource) ListChildren(namespace string) ([]Child, error) {
	type childKey struct{ name, typ string }
	seen := make(map[childKey]bool)
	moduleCount := make(map[string]int)
	var result []Child

	add := func(c Child) {
		if c.Type == "module" {
			moduleCount[c.Name]++
		}
		key := childKey{c.Name, c.Type}
		if seen[key] {
			return
		}
		seen[key] = true
		result = append(result, c)
	}

	addNamespace := func(name string) {
		add(Child{Name: name, Type: "namespace"})
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
				add(c)
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
				add(c)
			}
		}
	}

	// Модуль с одним именем из нескольких sources — конфликт.
	for _, c := range result {
		if c.Type == "module" && moduleCount[c.Name] > 1 {
			return nil, fmt.Errorf("discovery: conflict: module %q found in multiple sources", c.Name)
		}
	}

	return result, nil
}

// ListChildrenRecursive рекурсивно обходит поддерево namespace, объединяя
// результаты из всех sources. Обход делегируется каждому GlobalSource
// (защита от циклов симлинков — в нём); source, смонтированный ниже
// namespace, даёт синтетические namespace-записи для сегментов префикса.
// Слияние — как в ListChildren: ключ (имя, тип), source с ошибкой
// пропускается; модуль с одним именем из нескольких sources на любом
// уровне — ошибка конфликта.
func (cs *CompositeSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	var lists [][]Child
	moduleCount := make(map[string]int)

	for i, sc := range cs.configs {
		prefix := sc.FullPrefix()

		var relNs, mountRest string
		switch {
		case prefix == namespace:
			// Source смонтирован ровно тут — всё его дерево
		case namespace == "" && prefix != "":
			mountRest = prefix
		case strings.HasPrefix(prefix, namespace+"."):
			mountRest = prefix[len(namespace)+1:]
		case prefix == "":
			// Source содержит namespace в своём дереве
			relNs = namespace
		case strings.HasPrefix(namespace, prefix+"."):
			relNs = namespace[len(prefix)+1:]
		default:
			continue
		}

		var list []Child
		if mountRest != "" {
			// Source смонтирован ниже — namespace для каждого сегмента префикса
			segments := strings.Split(mountRest, ".")
			for j := range segments {
				list = append(list, Child{Name: strings.Join(segments[:j+1], "."), Type: "namespace"})
			}
		}

		children, err := cs.sources[i].ListChildrenRecursive(relNs)
		if err == nil {
			for _, c := range children {
				if mountRest != "" {
					c.Name = mountRest + "." + c.Name
				}
				if c.Type == "module" {
					moduleCount[c.Name]++
				}
				list = append(list, c)
			}
		}
		lists = append(lists, list)
	}

	result := mergeRecursive(lists)

	// Модуль с одним именем из нескольких sources — конфликт.
	for _, c := range result {
		if c.Type == "module" && moduleCount[c.Name] > 1 {
			return nil, fmt.Errorf("discovery: conflict: module %q found in multiple sources", c.Name)
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
