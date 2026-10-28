package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tivjnb/futi/internal/discovery"
)

// printedWarnings — ключи предупреждений (см. warningKey), уже выведенных в этом процессе:
// один и тот же нечитаемый source не должен предупреждать дважды (Start кэша и листинг).
var (
	printedWarningsMu sync.Mutex
	printedWarnings   = map[string]bool{}
	// warnedReads — первые *fs.PathError напечатанных предупреждений (под тем же
	// мьютексом): по ним итоговая ошибка с той же причиной сокращается.
	warnedReads []*fs.PathError
)

// printPartialWarnings печатает в w по строке `warning: <ошибка>` для каждой
// ошибки частичного листинга (*discovery.PartialError в цепочке err) и возвращает
// true. Уже выведенные в этом процессе тексты не повторяются. Для прочих ошибок
// (и nil) ничего не печатает и возвращает false.
func printPartialWarnings(w io.Writer, err error) bool {
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		return false
	}
	printedWarningsMu.Lock()
	defer printedWarningsMu.Unlock()
	for _, e := range pe.Errs {
		msg := e.Error()
		key := warningKey(e)
		if printedWarnings[key] {
			continue
		}
		printedWarnings[key] = true
		var pathErr *fs.PathError
		if errors.As(e, &pathErr) {
			warnedReads = append(warnedReads, pathErr)
		}
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
	return true
}

// warningKey — ключ дедупа предупреждений: первый *fs.PathError в цепочке
// (операция, путь, причина) — одна и та же проблема чтения приходит из разных
// листингов с разной обёрткой; без PathError — полный текст ошибки.
func warningKey(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Op + "\x00" + pathErr.Path + "\x00" + pathErr.Err.Error()
	}
	return err.Error()
}

// coveredByWarning сообщает, что ошибка уже объяснена напечатанным
// предупреждением: первый *fs.PathError в цепочке err имеет ту же причину, что и
// предупреждение, а пути совпадают или один лежит внутри другого. Операция
// (stat/open) не учитывается. Без *fs.PathError ошибка не покрыта.
func coveredByWarning(err error) bool {
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		return false
	}
	printedWarningsMu.Lock()
	defer printedWarningsMu.Unlock()
	for _, w := range warnedReads {
		if w.Err.Error() != pathErr.Err.Error() {
			continue
		}
		if pathWithin(w.Path, pathErr.Path) || pathWithin(pathErr.Path, w.Path) {
			return true
		}
	}
	return false
}

// pathWithin сообщает, что path совпадает с root или лежит внутри root.
func pathWithin(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	return path == root || strings.HasPrefix(path, strings.TrimRight(root, "/\\")+string(filepath.Separator))
}

// coveredReadMessage — короткое сообщение вместо ошибки, объяснённой предупреждением.
func coveredReadMessage(prefix, path string) string {
	return fmt.Sprintf("%s: cannot read namespace %q (see warning above)", prefix, path)
}
