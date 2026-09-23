package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"futi/internal/metayaml"
)

// sourceKind — тип источника модуля (спека §6).
type sourceKind int

const (
	kindLocal sourceKind = iota
	kindGit
	kindArchive
)

// String — имя типа для __futi_meta.yaml (lockfile.type).
func (k sourceKind) String() string {
	switch k {
	case kindLocal:
		return "local"
	case kindGit:
		return "git"
	case kindArchive:
		return "archive"
	}
	return "unknown"
}

// archiveExts — расширения web-архивов (спека §6.2).
var archiveExts = []string{".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".zip"}

// archiveExt возвращает расширение архива из строки (длиннейшее совпадение).
func archiveExt(src string) string {
	lower := strings.ToLower(src)
	for _, ext := range archiveExts {
		if strings.HasSuffix(lower, ext) {
			return ext
		}
	}
	return ""
}

// isArchiveURL проверяет, выглядит ли src как http(s)-URL архива.
func isArchiveURL(src string) bool {
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return false
	}
	return archiveExt(src) != ""
}

// isGitURL проверяет, выглядит ли src как git-репозиторий.
func isGitURL(src string) bool {
	if strings.HasSuffix(src, ".git") {
		return true
	}
	for _, host := range []string{"github.com", "gitlab.com", "bitbucket.org"} {
		if strings.Contains(src, host) && strings.Contains(src, "/") && !isArchiveURL(src) {
			return true
		}
	}
	return false
}

// detectSourceType определяет тип источника (спека §6.1).
// Приоритет: явные флаги --from-* > существующий путь (local) > git URL >
// archive URL. Два и более --from-* — ошибка.
func detectSourceType(src string, fromLocal, fromGit, fromArchive bool) (sourceKind, error) {
	specified := 0
	for _, b := range []bool{fromLocal, fromGit, fromArchive} {
		if b {
			specified++
		}
	}
	if specified > 1 {
		return 0, fmt.Errorf("only one of --from-local/--from-git/--from-archive is allowed")
	}
	switch {
	case fromLocal:
		return kindLocal, nil
	case fromGit:
		return kindGit, nil
	case fromArchive:
		return kindArchive, nil
	}

	if _, err := os.Stat(src); err == nil {
		return kindLocal, nil
	}
	if isGitURL(src) {
		return kindGit, nil
	}
	if isArchiveURL(src) {
		return kindArchive, nil
	}
	return 0, fmt.Errorf("cannot determine source type for %q; use --from-{local,git,archive}", src)
}

// fallbackName — имя модуля, если в config.yaml нет name: basename
// источника без хвостового "/", суффикса ".git" и расширения архива.
func fallbackName(src string) string {
	base := strings.TrimSuffix(strings.TrimSuffix(src, "/"), ".git")
	if ext := archiveExt(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return filepath.Base(base)
}

// isExcludedComponent — исключаемый компонент пути (metayaml.ExcludedDirs).
func isExcludedComponent(name string) bool {
	for _, ex := range metayaml.ExcludedDirs {
		if name == ex {
			return true
		}
	}
	return false
}

// copyTreeFiltered копирует дерево src в dst, пропуская excluded-каталоги.
// Копируем только regular-файлы (симлинки и прочее — пропускаем).
func copyTreeFiltered(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if d.IsDir() {
			if isExcludedComponent(d.Name()) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() || isExcludedComponent(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Сохраняем права файла (+x на скриптах модуля).
		mode := os.FileMode(0o644)
		if info, infoErr := d.Info(); infoErr == nil {
			mode = info.Mode()
		}
		return os.WriteFile(filepath.Join(dst, rel), data, mode)
	})
}

// moveTree переносит каталог src в dst: сначала os.Rename (атомарно);
// при ошибке (разные файловые системы, например /tmp → $HOME) —
// копирование copyTreeFiltered + удаление src.
func moveTree(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTreeFiltered(src, dst); err != nil {
		return fmt.Errorf("move tree: %w", err)
	}
	return os.RemoveAll(src)
}

// cleanupExcludedDirs удаляет excluded-каталоги из корня dest
// (например, .git после клонирования).
func cleanupExcludedDirs(dest string) error {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && isExcludedComponent(e.Name()) {
			if err := os.RemoveAll(filepath.Join(dest, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveModuleDir нормализует распакованное/скопированное дерево: если
// config.yaml нет в корне, но ровно один дочерний каталог содержит config.yaml —
// возвращаем его (архивы часто заворачивают содержимое в top-level каталог).
func resolveModuleDir(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "config.yaml")); err == nil {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || isExcludedComponent(e.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "config.yaml")); err == nil {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 {
		return filepath.Join(root, dirs[0]), nil
	}
	return "", fmt.Errorf("source is not a futi module: no config.yaml in %s", root)
}

// fetchLocalSource готовит модуль из локального источника в tempBase.
// Каталог — копируется с фильтром excluded, затем resolveModuleDir.
// Одиночный .yaml/.yml — заворачивается в пакет module/config.yaml
// (отклонение плана №3).
func fetchLocalSource(src, tempBase string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("local source: %w", err)
	}

	if !info.IsDir() {
		if !strings.HasSuffix(src, ".yaml") && !strings.HasSuffix(src, ".yml") {
			return "", fmt.Errorf("local source %q is neither a directory nor a .yaml file", src)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return "", err
		}
		moduleDir := filepath.Join(tempBase, "module")
		if err := os.MkdirAll(moduleDir, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(moduleDir, "config.yaml"), data, 0o644); err != nil {
			return "", err
		}
		return moduleDir, nil
	}

	dest := filepath.Join(tempBase, "module")
	if err := copyTreeFiltered(src, dest); err != nil {
		return "", fmt.Errorf("copy source: %w", err)
	}
	return resolveModuleDir(dest)
}

// fetchSource получает модуль из источника в tempBase (каталог вызывающего,
// удаляется им же через defer). Возвращает каталог модуля (возможно, сам
// tempBase-подкаталог — install делает os.Rename от него) и commit (git).
func fetchSource(ctx context.Context, src string, kind sourceKind, ref, tempBase string) (moduleDir, commit string, err error) {
	switch kind {
	case kindLocal:
		dir, err := fetchLocalSource(src, tempBase)
		return dir, "", err
	case kindGit:
		return fetchGit(ctx, src, ref, tempBase)
	case kindArchive:
		dir, err := fetchArchive(ctx, src, tempBase)
		return dir, "", err
	}
	return "", "", fmt.Errorf("unknown source kind: %d", kind)
}
