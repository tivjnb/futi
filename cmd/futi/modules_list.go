package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/termtext"

	"github.com/olekukonko/tablewriter"
)

// installedEntry — установленный модуль, найденный сканированием.
type installedEntry struct {
	Name string // dot-имя (namespace.name)
	Dir  string // каталог модуля (пакет)
	Meta *metayaml.Meta
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
// результат один раз — update не обновит его дважды. Ошибки чтения
// отдельного модуля или каталога не фатальны — он пропускается.
func scanRoots(roots []moduleRoot) (entries []installedEntry, backups []string) {
	seen := make(map[string]bool)
	for _, root := range roots {
		walkInstalled(root, root.Dir, "", make(map[string]bool), seen, &entries, &backups)
	}
	return entries, backups
}

// walkInstalled обходит каталог dir корня root (rel — его путь от корня).
// ancestors — реальные пути каталогов текущего пути обхода: симлинк на
// предка (цикл) не раскрывается. seen — реальные пути уже найденных модулей
// и backup-каталогов во всех корнях.
func walkInstalled(root moduleRoot, dir, rel string, ancestors, seen map[string]bool, entries *[]installedEntry, backups *[]string) {
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
		if strings.HasSuffix(child.Name(), backupSuffix) {
			if markSeen(seen, path) {
				*backups = append(*backups, path)
			}
			continue
		}
		if _, err := os.Stat(filepath.Join(path, "config.yaml")); err == nil {
			meta, metaErr := metayaml.Read(filepath.Join(path, metayaml.FileName))
			if metaErr == nil && markSeen(seen, path) {
				*entries = append(*entries, installedEntry{Name: prefixedName(root.Prefix, dotName(childRel)), Dir: path, Meta: meta})
			}
			continue // внутрь модуля не спускаемся
		}
		walkInstalled(root, path, childRel, ancestors, seen, entries, backups)
	}
}

// markSeen отмечает реальный путь path в seen; false — уже был отмечен.
func markSeen(seen map[string]bool, path string) bool {
	real := normalizePath(path)
	if seen[real] {
		return false
	}
	seen[real] = true
	return true
}

// scanInstalled — установленные модули из корней (без backup-каталогов).
func scanInstalled(roots []moduleRoot) []installedEntry {
	entries, _ := scanRoots(roots)
	return entries
}

// runModulesList выводит установленные модули (с __futi_meta.yaml).
func runModulesList(w io.Writer, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: futi modules list")
	}

	roots, err := readModuleRoots()
	if err != nil {
		return fmt.Errorf("modules list: %w", err)
	}
	entries := scanInstalled(roots)
	if len(entries) == 0 {
		fmt.Fprintln(w, "No installed modules found.")
		return nil
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	var rows [][]string
	for _, e := range entries {
		version := e.Meta.Lockfile.Ref
		if version == "" {
			version = e.Meta.Lockfile.Commit
			if len(version) > 12 {
				version = version[:12]
			}
		}
		installed := ""
		if !e.Meta.Lockfile.InstalledAt.IsZero() {
			installed = e.Meta.Lockfile.InstalledAt.Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{termtext.Line(e.Name), termtext.Line(e.Meta.Lockfile.Source), termtext.Line(version), installed})
	}

	tw := tablewriter.NewTable(w)
	tw.Header("NAME", "SOURCE", "VERSION", "INSTALLED_AT")
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("modules list: render: %w", err)
	}
	return tw.Render()
}
