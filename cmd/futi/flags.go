package main

import (
	"fmt"
	"strings"

	"futi/internal/execmodule"
)

// bindFlags привязывает сырые флаги (всё между путём и `--`) к параметрам
// команды по её схеме. Слева направо:
//   - --name=val — значение любого типа (неизвестные имена отвергает cmd.Validate);
//   - -h / --help — help=true сразу (до разбора остальных флагов);
//   - --name без "=": bool — "true", следующий токен не трогается; string/int —
//     следующий токен является значением, если он не начинается с "--"
//     (так "-h", "-5", "-v" и слова — значения); иначе ошибка;
//   - прочий токен с одним "-" и слово, не съеденное как значение, — ошибка.
//
// Повтор флага — последний выигрывает.
func bindFlags(cmd *execmodule.Command, flags []string) (params map[string]string, help bool, err error) {
	params = make(map[string]string)
	for i := 0; i < len(flags); i++ {
		tok := flags[i]

		if tok == "-h" || tok == "--help" {
			return nil, true, nil
		}

		if strings.HasPrefix(tok, "--") {
			name := strings.TrimPrefix(tok, "--")
			if idx := strings.Index(name, "="); idx >= 0 {
				if idx == 0 {
					return nil, false, fmt.Errorf("invalid flag %q", tok)
				}
				params[name[:idx]] = name[idx+1:]
				continue
			}

			p := findParam(cmd, name)
			if p == nil {
				return nil, false, fmt.Errorf("unknown param %q", name)
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
			return nil, false, fmt.Errorf("flag --%s requires a value (use --%s=<value>)", name, name)
		}

		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			return nil, false, fmt.Errorf("unknown flag %q: params are passed as --name value", tok)
		}

		return nil, false, fmt.Errorf("unexpected argument %q: the command path goes before flags, positional args after --", tok)
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
