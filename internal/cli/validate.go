package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// runValidate загружает модуль и выводит отчёт валидации схемы.
// Использует ParseModule (без валидации) + Validate() для получения полного
// списка ошибок и предупреждений за один проход.
// Возвращает error только если есть errors (warnings не блокируют).
func runValidate(w io.Writer, source discovery.Source, namespace string) error {
	rep, err := core.ValidateIn(source, namespace)
	if err != nil {
		return err
	}

	report, hasErrors := execmodule.FormatValidateReport(rep.Errors, rep.Warnings)
	fmt.Fprint(w, report)

	if hasErrors {
		return fmt.Errorf("validate: module %q has %d error(s)", namespace, len(rep.Errors))
	}
	return nil
}

// withValidateHint дописывает к ошибке резолва подсказку о переезде validate
// (`futi <ns> validate` → `futi modules validate <ns>`), если путь оканчивается
// словом validate и команда/модуль не найдены. Конкретный <ns> подставляется,
// только если он резолвится в существующий модуль; иначе — общая форма.
// Прочие ошибки (в том числе исполнения пользовательской команды validate)
// возвращаются как есть.
func withValidateHint(msg string, err error, path []string, source discovery.Source) string {
	if !errors.Is(err, modulerunner.ErrModuleNotFound) && !errors.Is(err, execmodule.ErrCommandNotFound) {
		return msg
	}
	if len(path) == 0 || path[len(path)-1] != "validate" {
		return msg
	}
	ns := "<namespace>"
	if len(path) > 1 {
		cand := strings.Join(path[:len(path)-1], ".")
		if _, _, rerr := source.Resolve(cand); rerr == nil {
			ns = cand
		}
	}
	return msg + "\nhint: use 'futi modules validate " + ns + "'"
}
