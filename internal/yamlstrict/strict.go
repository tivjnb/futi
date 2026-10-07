// Пакет yamlstrict проверяет дерево YAML на ключи, которых нет в целевой структуре.
//
// yaml.Unmarshal молча отбрасывает неизвестные ключи, поэтому опечатка в
// конфиге или модуле превращается в тихое поведение по умолчанию. Здесь
// проверка идёт по yaml.Node, чтобы в ошибках были номера строк.
package yamlstrict

import (
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// extensionPrefix — префикс ключей-расширений: они разрешены в любом mapping,
// соответствующем структуре, и не проверяются.
const extensionPrefix = "x-"

// UnknownKeys проверяет дерево YAML против yaml-тегов типа target (struct
// или указатель на struct) и возвращает по ошибке на каждый неизвестный ключ
// в порядке документа. Ключи с префиксом "x-" в mapping, соответствующем
// структуре, разрешены и не проверяются (на любом уровне).
func UnknownKeys(root *yaml.Node, target reflect.Type) []error {
	if root == nil {
		return nil
	}
	w := &walker{seen: make(map[visit]bool)}
	w.walk(root, target, "")
	return w.errs
}

// visit — пара «узел + тип»: один и тот же узел (якорь) проверяется против
// одного типа только раз. Это защищает и от экспоненциального обхода алиасов,
// и от бесконечной рекурсии по рекурсивным типам.
type visit struct {
	node *yaml.Node
	typ  reflect.Type
}

type walker struct {
	seen map[visit]bool
	errs []error
}

func (w *walker) walk(node *yaml.Node, typ reflect.Type, path string) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, c := range node.Content {
			w.walk(c, typ, path)
		}
		return
	case yaml.AliasNode:
		if node.Alias != nil {
			w.walk(node.Alias, typ, path)
		}
		return
	case yaml.MappingNode, yaml.SequenceNode:
		v := visit{node, typ}
		if w.seen[v] {
			return
		}
		w.seen[v] = true
	default:
		return
	}

	switch {
	case node.Kind == yaml.MappingNode && typ.Kind() == reflect.Struct:
		w.walkStruct(node, typ, path)
	case node.Kind == yaml.MappingNode && typ.Kind() == reflect.Map:
		for i := 0; i+1 < len(node.Content); i += 2 {
			w.walk(node.Content[i+1], typ.Elem(), joinKey(path, node.Content[i].Value))
		}
	case node.Kind == yaml.SequenceNode && (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array):
		for i, c := range node.Content {
			w.walk(c, typ.Elem(), fmt.Sprintf("%s[%d]", path, i))
		}
	}
	// Остальное (скаляры, несовпадение вида узла и типа) ловит yaml.Unmarshal.
}

func (w *walker) walkStruct(node *yaml.Node, typ reflect.Type, path string) {
	fields := structFields(typ)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		if key.Tag == "!!merge" {
			w.walkMerge(val, typ, path)
			continue
		}
		if ft, ok := fields[key.Value]; ok {
			w.walk(val, ft, joinKey(path, key.Value))
			continue
		}
		if strings.HasPrefix(key.Value, extensionPrefix) {
			continue
		}
		msg := fmt.Sprintf("line %d: unknown key %q", key.Line, key.Value)
		if path != "" {
			msg += " in " + path
		}
		w.errs = append(w.errs, fmt.Errorf("%s", msg))
	}
}

// walkMerge проверяет значение merge-ключа (<<): mapping, алиас на mapping или
// список таковых — против той же структуры, что и содержащий их mapping.
func (w *walker) walkMerge(val *yaml.Node, typ reflect.Type, path string) {
	if val.Kind == yaml.SequenceNode {
		for _, c := range val.Content {
			w.walk(c, typ, path)
		}
		return
	}
	w.walk(val, typ, path)
}

// structFields возвращает yaml-имена полей структуры и их типы так же, как
// yaml.v3: тег до запятой, иначе имя поля в нижнем регистре; "-" и
// неэкспортируемые поля пропускаются; ",inline" поднимает поля встроенной структуры.
func structFields(typ reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	collectFields(typ, fields)
	return fields
}

func collectFields(typ reflect.Type, fields map[string]reflect.Type) {
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if hasOption(opts, "inline") {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectFields(ft, fields)
			}
			continue
		}
		if f.PkgPath != "" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields[name] = f.Type
	}
}

func hasOption(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == want {
			return true
		}
	}
	return false
}

func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
