package modules

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/metayaml"
)

// Entry — установленный модуль, найденный сканированием.
type Entry struct {
	Name    string // dot-имя (namespace.name)
	Dir     string // каталог модуля (пакет)
	Meta    *metayaml.Meta
	ViaLink bool // путь проходит через симлинк-каталог ниже корня
	// SelfLink — сам каталог модуля (последний элемент пути) симлинк.
	SelfLink bool
}

// dotName превращает относительный путь ("images/qr") в dot-имя ("images.qr").
func dotName(rel string) string {
	return strings.ReplaceAll(filepath.ToSlash(rel), "/", ".")
}

// backupSuffix — суффикс каталога с копией старого дерева на время update.
const backupSuffix = ".futi-backup"

// scanRoots обходит корни и собирает пакеты с config.yaml +
// __futi_meta.yaml, а также каталоги *.futi-backup, оставшиеся после
// прерванного update (backup — не модуль, внутрь не спускаемся). Внутрь
// найденного модуля не спускаемся. Имя модуля — с префиксом корня, как его
// резолвит discovery. Симлинки на каталоги (и корень-симлинк) раскрываются,
// как в discovery; один реальный каталог модуля или backup попадает в
// результат один раз — update не обновит его дважды. Из нескольких путей к
// нему выбирается найденный без симлинк-каталога (ViaLink == false), иначе —
// первый по обходу; порядок — по первому появлению, он детерминирован
// (корни по порядку, записи каталога по имени). Ошибки чтения отдельного
// модуля или каталога не фатальны — он пропускается.
func scanRoots(roots []moduleRoot) (entries []Entry, backups []string) {
	st := scanState{entryIdx: make(map[string]int), backupIdx: make(map[string]int)}
	for _, root := range roots {
		st.walk(root, root.Dir, "", false, make(map[string]bool))
	}
	for _, b := range st.backups {
		backups = append(backups, b.Dir)
	}
	return st.entries, backups
}

// scanState — накопленный результат scanRoots; *Idx — реальный путь
// каталога → индекс записи (Name и Meta у backup-записей не заполняются).
type scanState struct {
	entries   []Entry
	entryIdx  map[string]int
	backups   []Entry
	backupIdx map[string]int
}

// walk обходит каталог dir корня root (rel — его путь от корня, viaLink —
// путь уже прошёл через симлинк-каталог ниже корня). ancestors — реальные
// пути каталогов текущего пути обхода: симлинк на предка (цикл) не
// раскрывается.
func (st *scanState) walk(root moduleRoot, dir, rel string, viaLink bool, ancestors map[string]bool) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || ancestors[real] {
		return
	}
	ancestors[real] = true
	defer delete(ancestors, real)

	children, err := os.ReadDir(dir)
	if err != nil {
		return // недоступное — пропускаем
	}
	for _, child := range children {
		path := filepath.Join(dir, child.Name())
		childRel := filepath.Join(rel, child.Name())
		info, err := os.Stat(path) // за симлинком
		if err != nil || !info.IsDir() || metayaml.IsExcluded(childRel) {
			continue
		}
		childViaLink := viaLink || child.Type()&fs.ModeSymlink != 0
		if strings.HasSuffix(child.Name(), backupSuffix) {
			keepPreferred(&st.backups, st.backupIdx, Entry{Dir: path, ViaLink: childViaLink})
			continue
		}
		if _, err := os.Stat(filepath.Join(path, "config.yaml")); err == nil {
			if meta, err := metayaml.Read(filepath.Join(path, metayaml.FileName)); err == nil {
				keepPreferred(&st.entries, st.entryIdx, Entry{
					Name: prefixedName(root.Prefix, dotName(childRel)), Dir: path, Meta: meta, ViaLink: childViaLink,
					SelfLink: child.Type()&fs.ModeSymlink != 0,
				})
			}
			continue // внутрь модуля не спускаемся
		}
		st.walk(root, path, childRel, childViaLink, ancestors)
	}
}

// keepPreferred добавляет e в *list, если его реальный каталог ещё не
// встречался; встречался через симлинк-каталог, а e найден без него — e
// заменяет прежнюю запись на её месте.
func keepPreferred(list *[]Entry, idx map[string]int, e Entry) {
	real := normalizePath(e.Dir)
	i, ok := idx[real]
	if !ok {
		idx[real] = len(*list)
		*list = append(*list, e)
		return
	}
	if (*list)[i].ViaLink && !e.ViaLink {
		(*list)[i] = e
	}
}

// scanInstalled — установленные модули из корней (без backup-каталогов).
func scanInstalled(roots []moduleRoot) []Entry {
	entries, _ := scanRoots(roots)
	return entries
}

// List returns the installed modules (those with __futi_meta.yaml) sorted by
// Name.
func (m *Manager) List() ([]Entry, error) {
	roots, err := m.moduleRoots()
	if err != nil {
		return nil, fmt.Errorf("modules list: %w", err)
	}
	entries := scanInstalled(roots)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}
