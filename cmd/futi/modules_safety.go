package main

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// validModuleNameRe — допустимые имена модулей (спека §8 #3): первый символ
// буква/цифра/_; далее также ".", "/" и "-". Точка и слэш разрешены только
// внутри (namespace-пути вида images/qr), ведущая точка и ".." отвергаются.
var validModuleNameRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_./-]*$`)

// validateModuleName проверяет имя модуля (в т.ч. namespace-путь).
func validateModuleName(name string) error {
	if !validModuleNameRe.MatchString(name) {
		return fmt.Errorf("invalid module name %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		// Пустой сегмент — это "a//b" или хвостовой "/" в "a/":
		// нормализуется filepath.Join непредсказуемо, отвергаем.
		if seg == "" || seg == ".." || seg == "." {
			return fmt.Errorf("invalid module name %q", name)
		}
	}
	return nil
}

// ensureWithinRoot проверяет, что path (после резолва симлинков) находится
// внутри одного из roots (спека §8 #2, #12). Используется перед удалением.
func ensureWithinRoot(path string, roots []string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	for _, root := range roots {
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(realRoot, real)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("path %q is outside known futi modules directories", path)
}

// countTree считает файлы и суммарный размер дерева (для summary удаления).
func countTree(root string) (files int, size int64) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		files++
		size += info.Size()
		return nil
	})
	return files, size
}

// confirmDelete спрашивает подтверждение: summary + "Delete? [y/N]".
// r — stdin (в тестах подменяется), w — вывод prompt.
func confirmDelete(r io.Reader, w io.Writer, path string, files int, size int64) (bool, error) {
	fmt.Fprintf(w, "About to delete: %s (%d files, %d KB)\n", path, files, size/1024)
	fmt.Fprintf(w, "Delete? [y/N] ")

	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}
