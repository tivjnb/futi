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

// withValidateHint appends a hint about the move of validate
// (`futi <ns> validate` → `futi modules validate <ns>`) when err is an unknown
// path word and that word or the last of the leading words path is
// "validate". The concrete <ns> is used only if the unknown word is validate
// and the node before it is an existing module; otherwise the generic form.
// Other errors (including a run of a user command validate) are returned as is.
func withValidateHint(msg string, err error, path []string, source discovery.Source) string {
	var pathErr *modulerunner.PathError
	if !errors.As(err, &pathErr) || pathErr.Word == "" {
		return msg
	}
	if pathErr.Word != "validate" && (len(path) == 0 || path[len(path)-1] != "validate") {
		return msg
	}
	ns := "<namespace>"
	if pathErr.Word == "validate" && len(pathErr.Path) > 0 {
		cand := strings.Join(pathErr.Path, ".")
		if _, _, rerr := source.Resolve(cand); rerr == nil {
			ns = cand
		}
	}
	return msg + "\nhint: use 'futi modules validate " + ns + "'"
}
