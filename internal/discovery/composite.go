package discovery

import (
	"errors"
	"fmt"
	"os"
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

// checkRoot проверяет, что RootDir source-а i — существующий каталог. Иначе
// ошибка добавляется в errs.soft и возвращается false: такой source ничего не
// даёт в результат. Все sources CompositeSource заданы в config.yaml явно,
// поэтому отсутствующий каталог — это ошибка, а не «свежая установка».
func (cs *CompositeSource) checkRoot(i int, errs *listingErrs) bool {
	root := cs.sources[i].RootDir
	info, err := os.Stat(root)
	if err != nil {
		errs.soft = append(errs.soft, fmt.Errorf("discovery: source %q: %w", root, err))
		return false
	}
	if !info.IsDir() {
		errs.soft = append(errs.soft, fmt.Errorf("discovery: source %q: not a directory", root))
		return false
	}
	return true
}

// listingErrs — ошибки листинга CompositeSource. soft всегда уходят в
// *PartialError (ошибки stat корня source, ошибки частичных листингов);
// hard (прочие ошибки делегированного листинга) — в *PartialError, только
// если хотя бы один source дал результат, иначе возвращаются обычной ошибкой.
type listingErrs struct{ soft, hard []error }

// acceptListing разбирает ошибку делегированного листинга source-а i.
// Возвращает use=true, если результат листинга нужно использовать, и
// found=true, если source дал результат (nil или *PartialError):
// ErrNotFound — source не содержит namespace (не ошибка);
// *PartialError — результат берётся, ошибки добавляются в errs.soft;
// любая другая ошибка добавляется в errs.hard, source пропускается.
func (cs *CompositeSource) acceptListing(i int, err error, errs *listingErrs) (use, found bool) {
	if err == nil {
		return true, true
	}
	if errors.Is(err, ErrNotFound) {
		return false, false
	}
	var pe *PartialError
	if errors.As(err, &pe) {
		errs.soft = append(errs.soft, pe.Errs...)
		return true, true
	}
	errs.hard = append(errs.hard, fmt.Errorf("discovery: source %q: %w", cs.sources[i].RootDir, err))
	return false, false
}

// markSourceConflicts помечает модули, найденные в нескольких sources:
// Conflict = true и ConflictSources — RootDir этих sources в порядке
// конфигурации. moduleSources — по полному имени модуля.
func markSourceConflicts(result []Child, moduleSources map[string][]string) {
	for i := range result {
		c := &result[i]
		if c.Type == "module" && len(moduleSources[c.Name]) > 1 {
			c.Conflict = true
			c.ConflictSources = moduleSources[c.Name]
		}
	}
}

// listingResult собирает итог листинга.
// Есть результат (found) или namespace пуст (корень не бывает «не найден»):
// результат и *PartialError со всеми ошибками (nil без ошибок).
// Иначе результата нет: без ошибок — ErrNotFound; с ошибками — они
// возвращаются обычной ошибкой (errors.Join, не *PartialError): результат
// невалиден, namespace мог быть в недоступном source.
func listingResult(namespace string, found bool, result []Child, errs listingErrs) ([]Child, error) {
	all := append(append([]error(nil), errs.soft...), errs.hard...)
	if found || namespace == "" {
		return result, newPartialError(all)
	}
	switch len(all) {
	case 0:
		return nil, newNamespaceNotFound(namespace)
	case 1:
		return nil, all[0]
	default:
		return nil, errors.Join(all...)
	}
}

// ListChildren возвращает непосредственных детей namespace, объединяя
// результаты из всех sources. Ключ слияния — пара (имя, тип): модуль x и
// namespace x сосуществуют, как и в Resolve (x — модуль, x.y — внутри
// namespace). Если два source дают модуль с одинаковым именем — запись
// помечается Conflict, ConflictSources перечисляет их RootDir (Resolve для
// него вернёт ошибку конфликта).
// Недоступный source (нет каталога, не каталог, ошибка листинга) не роняет
// листинг: остальные sources отдают результат, ошибки — в *PartialError.
// Если ни один source не дал результата для namespace (не корня): без ошибок —
// ErrNotFound; с ошибками (stat корней, ошибки листинга) — они возвращаются
// обычной ошибкой (errors.Join), не *PartialError. Ошибки делегированного
// листинга (кроме ErrNotFound) при наличии результата от другого source
// уходят в *PartialError.
func (cs *CompositeSource) ListChildren(namespace string) ([]Child, error) {
	type childKey struct{ name, typ string }
	seen := make(map[childKey]bool)
	moduleSources := make(map[string][]string)
	var result []Child
	var errs listingErrs
	found := false

	add := func(c Child, root string) {
		if c.Type == "module" {
			moduleSources[c.Name] = append(moduleSources[c.Name], root)
		}
		key := childKey{c.Name, c.Type}
		if seen[key] {
			return
		}
		seen[key] = true
		result = append(result, c)
	}

	for i, sc := range cs.configs {
		prefix := sc.FullPrefix()
		root := cs.sources[i].RootDir

		switch {
		case prefix == namespace:
			// Source смонтирован ровно тут — его прямые дети
			if !cs.checkRoot(i, &errs) {
				continue
			}
			children, err := cs.sources[i].ListChildren("")
			use, ok := cs.acceptListing(i, err, &errs)
			found = found || ok
			if !use {
				continue
			}
			for _, c := range children {
				add(c, root)
			}

		case namespace == "" && prefix != "":
			// Листинг корня, source не в корне — первый сегмент = child namespace
			if !cs.checkRoot(i, &errs) {
				continue
			}
			found = true
			add(Child{Name: strings.SplitN(prefix, ".", 2)[0], Type: "namespace"}, root)

		case strings.HasPrefix(prefix, namespace+"."):
			// Source смонтирован ниже — следующий сегмент = child namespace
			if !cs.checkRoot(i, &errs) {
				continue
			}
			found = true
			rest := prefix[len(namespace)+1:]
			add(Child{Name: strings.SplitN(rest, ".", 2)[0], Type: "namespace"}, root)

		case prefix == "" || strings.HasPrefix(namespace, prefix+"."):
			// Source содержит namespace в своём дереве
			if !cs.checkRoot(i, &errs) {
				continue
			}
			relNs := namespace
			if prefix != "" {
				relNs = namespace[len(prefix)+1:]
			}
			children, err := cs.sources[i].ListChildren(relNs)
			use, ok := cs.acceptListing(i, err, &errs)
			found = found || ok
			if !use {
				continue
			}
			for _, c := range children {
				add(c, root)
			}
		}
	}

	markSourceConflicts(result, moduleSources)
	return listingResult(namespace, found, result, errs)
}

// ListChildrenRecursive рекурсивно обходит поддерево namespace, объединяя
// результаты из всех sources. Обход делегируется каждому GlobalSource
// (защита от циклов симлинков — в нём); source, смонтированный ниже
// namespace, даёт синтетические namespace-записи для сегментов префикса.
// Слияние, обработка недоступных sources и отсутствующего namespace — как в
// ListChildren; модуль с одним именем из нескольких sources на любом уровне
// помечается Conflict.
func (cs *CompositeSource) ListChildrenRecursive(namespace string) ([]Child, error) {
	var lists [][]Child
	moduleSources := make(map[string][]string)
	var errs listingErrs
	found := false

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

		if !cs.checkRoot(i, &errs) {
			continue
		}
		if mountRest != "" || prefix == namespace {
			found = true
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
		use, ok := cs.acceptListing(i, err, &errs)
		found = found || ok
		if use {
			for _, c := range children {
				if mountRest != "" {
					c.Name = mountRest + "." + c.Name
				}
				if c.Type == "module" {
					moduleSources[c.Name] = append(moduleSources[c.Name], cs.sources[i].RootDir)
				}
				list = append(list, c)
			}
		}
		lists = append(lists, list)
	}

	result := mergeRecursive(lists)
	markSourceConflicts(result, moduleSources)
	return listingResult(namespace, found, result, errs)
}

// List возвращает все модули из всех sources. Недоступный source не роняет
// листинг: остальные отдают модули, ошибки — в *PartialError.
// Примечание: в CLI сейчас используется ListChildren/ListChildrenRecursive;
// List оставлен для обратной совместимости и тестов.
func (cs *CompositeSource) List() ([]ModuleInfo, error) {
	var modules []ModuleInfo
	var errs listingErrs
	for i, sc := range cs.configs {
		if !cs.checkRoot(i, &errs) {
			continue
		}
		srcModules, err := cs.sources[i].List()
		if use, _ := cs.acceptListing(i, err, &errs); !use {
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
	return modules, newPartialError(append(errs.soft, errs.hard...))
}
