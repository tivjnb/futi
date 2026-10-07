package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tivjnb/futi/internal/completion"
	"github.com/tivjnb/futi/internal/termtext"
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

// checkReservedSegments отвергает путь, в котором любой сегмент (разделитель
// sep) — зарезервированное слово корня: такой модуль перехватывался бы
// builtin-командами и не был бы вызван. Проверяются только новые пути
// (init/install); уже установленные модули не трогаются.
func checkReservedSegments(path, sep string) error {
	for _, seg := range strings.Split(path, sep) {
		if completion.IsReservedName(seg) {
			return fmt.Errorf("%q is a reserved name, choose a different module name", seg)
		}
	}
	return nil
}

// validateSegmentsNoDot отвергает точку в сегментах пути размещения модуля
// (имя при install, путь при init): точка — разделитель namespace, модуль
// в каталоге/файле с точкой в имени discovery не резолвит.
func validateSegmentsNoDot(name string) error {
	if strings.Contains(name, ".") {
		return fmt.Errorf("invalid module name %q: точка зарезервирована как разделитель namespace", name)
	}
	return nil
}

// validateResolveName проверяет имя, которое пойдёт в Source.Resolve
// (uninstall, info, path, update, add_command, remove_command): namespace
// только в точечной форме (a.b). Слэш запрещён: Resolve режет имя по ".",
// и "dev/config" обходил бы проверки пакета/сегмента config, а uninstall
// удалял бы весь каталог. Слэш остаётся допустимым для install/init —
// там имя задаёт путь размещения и в Resolve не попадает.
func validateResolveName(name string) error {
	if err := validateModuleName(name); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("invalid module name %q: namespace записывается через точку (a.b), не через /", name)
	}
	return nil
}

// normalizePath приводит путь к абсолютному чистому виду и по возможности
// резолвит симлинки (если путь не существует — остаётся Abs+Clean).
func normalizePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// strictlyInside сообщает, лежит ли path строго внутри root (path != root).
// Оба пути должны быть уже нормализованы.
func strictlyInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ensureWithinRoot проверяет, что path (после резолва симлинков) находится
// строго внутри одного из roots (спека §8 #2, #12): сам корень не допускается.
// Используется перед удалением.
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
		if strictlyInside(realRoot, real) {
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

// stdinIsTerminal сообщает, подключён ли stdin к терминалу; шов для тестов.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// errAttachNotConfirmed — пользователь не подтвердил attach-хук.
var errAttachNotConfirmed = errors.New("attach hook not confirmed")

// attachGate решает, запускать ли attach-хук: --skip-attach важнее --yes,
// без обоих флагов хук запускается только после подтверждения в терминале.
// in — один reader stdin на весь вызов команды: update без имени спрашивает
// про каждый модуль, а свой буфер на каждый вопрос съел бы следующие ответы.
type attachGate struct {
	in   *bufio.Reader
	skip bool
	yes  bool
}

// allow возвращает, запускать ли хук attach модуля name в каталоге dir.
// Отказ пользователя — errAttachNotConfirmed. Без терминала на stdin ответ
// не читается: молчаливый отказ скрыл бы причину, а чтение из пайпа
// подтвердило бы хук данными, не предназначенными для этого вопроса.
func (g attachGate) allow(attach, name, dir string) (bool, error) {
	if attach == "" || g.skip {
		return false, nil
	}
	if g.yes {
		return true, nil
	}
	if !stdinIsTerminal() {
		return false, errors.New("attach hook requires confirmation: rerun with --yes to run it or --skip-attach to skip it")
	}
	// Показываем сырой шаблон: в отрендеренном могли бы оказаться значения
	// {{.env.X}}. Управляющие символы из чужого манифеста — вырезаем.
	fmt.Fprintf(os.Stderr, "Module %s runs an attach hook (in %s):\n", termtext.Line(name), termtext.Line(dir))
	for _, line := range strings.Split(strings.TrimRight(termtext.Block(attach), "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	fmt.Fprint(os.Stderr, "Run it? [y/N] ")

	line, err := g.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, errAttachNotConfirmed
}
