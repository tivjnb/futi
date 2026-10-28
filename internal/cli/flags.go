package cli

import (
	"fmt"
	"strings"

	"github.com/tivjnb/futi/internal/execmodule"
)

// bindFlags привязывает сырые флаги (всё между путём и `--`) к параметрам
// команды по её схеме. Слева направо:
//   - -h / --help вне позиции значения — help=true (выигрывает и у более
//     ранних ошибок привязки: сканирование после ошибки продолжается);
//   - --name=val — значение любого типа; имя должно быть в схеме;
//   - --name без "=": bool — "true", следующий токен не трогается; string/int —
//     следующий токен является значением, если он не начинается с "--"
//     (так "-h", "-5", "-v" и слова — значения); иначе ошибка;
//   - прочий токен с одним "-" и слово, не съеденное как значение, — ошибка.
//
// Без help возвращается первая ошибка. Повтор флага — последний выигрывает.
func bindFlags(cmd *execmodule.Command, flags []string) (params map[string]string, help bool, err error) {
	params = make(map[string]string)
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
		}
	}
	for i := 0; i < len(flags); i++ {
		tok := flags[i]

		if tok == "-h" || tok == "--help" {
			return nil, true, nil
		}

		if strings.HasPrefix(tok, "--") {
			name := strings.TrimPrefix(tok, "--")
			if idx := strings.Index(name, "="); idx >= 0 {
				if idx == 0 {
					fail(fmt.Errorf("invalid flag %q", tok))
					continue
				}
				if findParam(cmd, name[:idx]) == nil {
					fail(fmt.Errorf("unknown param %q", name[:idx]))
					continue
				}
				params[name[:idx]] = name[idx+1:]
				continue
			}

			p := findParam(cmd, name)
			if p == nil {
				fail(fmt.Errorf("unknown param %q", name))
				continue
			}
			if p.Type == "bool" {
				params[name] = "true"
				continue
			}
			if i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "--") {
				params[name] = flags[i+1]
				i++
				continue
			}
			fail(fmt.Errorf("flag --%s requires a value (use --%s=<value>)", name, name))
			continue
		}

		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			fail(fmt.Errorf("unknown flag %q: params are passed as --name value", tok))
			continue
		}

		fail(fmt.Errorf("unexpected argument %q: the command path goes before flags, positional args after --", tok))
	}
	if firstErr != nil {
		return nil, false, firstErr
	}
	return params, false, nil
}

// findParam возвращает параметр схемы команды по имени или nil.
func findParam(cmd *execmodule.Command, name string) *execmodule.Param {
	for i := range cmd.Params {
		if cmd.Params[i].Name == name {
			return &cmd.Params[i]
		}
	}
	return nil
}
