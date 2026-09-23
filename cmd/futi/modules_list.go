package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"futi/internal/config"
	"futi/internal/metayaml"

	"github.com/olekukonko/tablewriter"
)

// installedEntry — установленный модуль, найденный сканированием.
type installedEntry struct {
	Name string // dot-имя (namespace.name)
	Dir  string // каталог модуля (пакет)
	Meta *metayaml.Meta
}

// moduleRoots возвращает корни поиска установленных модулей: глобальный
// каталог (FUTI_MODULES_DIR или default) + ./.futi (если существует).
func moduleRoots() ([]string, error) {
	globalDir := os.Getenv("FUTI_MODULES_DIR")
	if globalDir == "" {
		dir, err := config.DefaultModulesDir()
		if err != nil {
			return nil, err
		}
		globalDir = dir
	}
	roots := []string{globalDir}

	if info, err := os.Stat(".futi"); err == nil && info.IsDir() {
		if abs, err := filepath.Abs(".futi"); err == nil {
			roots = append(roots, abs)
		}
	}
	return roots, nil
}

// dotName превращает относительный путь ("images/qr") в dot-имя ("images.qr").
func dotName(rel string) string {
	return strings.ReplaceAll(filepath.ToSlash(rel), "/", ".")
}

// scanInstalled обходит корни и собирает пакеты с config.yaml +
// __futi_meta.yaml. Внутрь найденного модуля не спускаемся (SkipDir).
// Ошибки чтения отдельного модуля не фатальны — модуль пропускается.
func scanInstalled(roots []string) []installedEntry {
	var found []installedEntry
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil // недоступное — пропускаем
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil || rel == "." {
				return nil
			}
			if metayaml.IsExcluded(rel) {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, "config.yaml")); statErr == nil {
				if meta, metaErr := metayaml.Read(filepath.Join(path, metayaml.FileName)); metaErr == nil {
					found = append(found, installedEntry{Name: dotName(rel), Dir: path, Meta: meta})
				}
				return filepath.SkipDir // внутрь модуля не спускаемся
			}
			return nil
		})
	}
	return found
}

// runModulesList выводит установленные модули (с __futi_meta.yaml).
func runModulesList(w io.Writer, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: futi modules list")
	}

	roots, err := moduleRoots()
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
		rows = append(rows, []string{e.Name, e.Meta.Lockfile.Source, version, installed})
	}

	tw := tablewriter.NewTable(w)
	tw.Header("NAME", "SOURCE", "VERSION", "INSTALLED_AT")
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("modules list: render: %w", err)
	}
	return tw.Render()
}
