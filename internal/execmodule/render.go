package execmodule

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"
)

// Validate проверяет переданные параметры против схемы команды.
// Применяет defaults. Для параметров с Type=="bool" нормализует значения
// (см. normalizeBool), чтобы {{if .bool}} в text/template работало корректно:
// falsy-строки приводятся к "", остальные — к "true".
// Возвращает map с итоговыми значениями (включая defaults) или ошибку
// с понятным сообщением.
func (c *Command) Validate(provided map[string]string) (map[string]string, error) {
	resolved := make(map[string]string)

	for _, p := range c.Params {
		val, ok := provided[p.Name]
		if !ok {
			if p.Default != "" {
				val = p.Default
			} else if p.Required {
				return nil, fmt.Errorf("execmodule: required param %q missing", p.Name)
			}
		}
		if p.Type == "bool" {
			val = normalizeBool(val)
		}
		if p.Type == "int" && val != "" {
			if _, err := strconv.Atoi(val); err != nil {
				return nil, fmt.Errorf("execmodule: param %q must be int, got %q", p.Name, val)
			}
		}
		resolved[p.Name] = val
	}

	for name := range provided {
		known := false
		for _, p := range c.Params {
			if p.Name == name {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("execmodule: unknown param %q", name)
		}
	}

	return resolved, nil
}

// ValidatePositional проверяет количество позиционных аргументов (после "--")
// против требования positional.min. Возвращает ошибку с понятным сообщением,
// если аргументов меньше минимума. При min==0 (не задано) — всегда проходит.
func (c *Command) ValidatePositional(argsList []string) error {
	if c.Positional.Min == 0 {
		return nil
	}
	if len(argsList) < c.Positional.Min {
		noun := "argument"
		if c.Positional.Min != 1 {
			noun = "arguments"
		}
		return fmt.Errorf(
			"command requires at least %d positional %s after \"--\", got %d",
			c.Positional.Min, noun, len(argsList),
		)
	}
	return nil
}

// ModuleRef — информация о модуле для шаблонного контекста cmd-рендера.
// Используется в Command.Render как параметр module: добавляет в данные
// шаблона переменные {{.module_name}} и {{.module_root}} (WORKDIR.md §3.2).
//
// Zero-value ModuleRef (пустые Name и Root) НЕ заполняет переменные в шаблоне;
// обращение к {{.module_root}} или {{.module_name}} даст missingkey-ошибку
// (fail-fast, как у {{.env.X}} при отсутствии env var).
type ModuleRef struct {
	Name string
	Root string
}

// TemplateError — ошибка парсинга или выполнения cmd-шаблона.
// Несёт сам шаблон и системную причину. CLI форматирует её дружелюбно,
// скрывая длинную цепочку обёрток и технический трейс text/template.
type TemplateError struct {
	Cmd   string
	Cause error
}

// Error возвращает короткое сообщение с шаблоном и причиной.
func (e *TemplateError) Error() string {
	return fmt.Sprintf("failed to render template %q: %v", e.Cmd, e.Cause)
}

// Unwrap возвращает системную причину (для errors.As/Is).
func (e *TemplateError) Unwrap() error { return e.Cause }

// Render подставляет значения параметров, args и env в cmd-шаблон.
// Использует Go text/template: {{.var}} для подстановки params,
// {{.args}} для строки args, {{index .args_list N}} для элемента массива args,
// {{if .bool}}...{{end}} для условных блоков.
// {{.env.FOO}} — доступ к переменной окружения FOO (error если не задана,
// для optional-проверки используйте {{if index .env "FOO"}}).
// {{.module_root}}/{{.module_name}} — путь и имя модуля (задаются параметром
// module; zero-value ModuleRef → missingkey-ошибка при обращении).
// {{quote .x}} — безопасная вставка значения как ОДНОГО argv-токена
// (экранирует кавычки/пробелы/метасимволы, см. quoteShlex).
// Ошибки парсинга и выполнения шаблона возвращаются как *TemplateError.
func (c *Command) Render(params map[string]string, args string, argsList []string, module ModuleRef) (string, error) {
	tmpl, err := template.New("cmd").
		Funcs(template.FuncMap{"quote": quoteShlex}).
		Option("missingkey=error").
		Parse(c.Cmd)
	if err != nil {
		return "", &TemplateError{Cmd: c.Cmd, Cause: err}
	}

	data := make(map[string]interface{}, len(params)+5)
	for k, v := range params {
		data[k] = v
	}
	// Служебные ключи пишутся после params; коллизию имён исключает Validate.
	data["args"] = args
	if argsList == nil {
		argsList = []string{}
	}
	data["args_list"] = argsList
	data["env"] = envMapFn()
	if module.Name != "" {
		data["module_name"] = module.Name
	}
	if module.Root != "" {
		data["module_root"] = module.Root
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", &TemplateError{Cmd: c.Cmd, Cause: err}
	}

	return buf.String(), nil
}

// quoteShlex возвращает s в форме, которую shlex.Split интерпретирует как
// один argv-токен, даже если s содержит пробелы, кавычки или метасимволы.
// POSIX-стиль: обёртка в одинарные кавычки; каждая внутренняя одинарная
// кавычка заменяется на последовательность (закрывающая кавычка,
// экранированная кавычка, открывающая кавычка):
//
//	'\''
//
// Назначение (REV-004): cmd-шаблон рендерится в строку и затем разбивается
// shlex.Split, поэтому значение, вставленное как {{.msg}}, интерпретируется
// shlex-ом повторно — кавычки из значения пробивают границы argv и позволяют
// пользователю добавить произвольные флаги (--force, --amend). Функция
// {{quote .msg}} в шаблоне экранирует значение так, что оно гарантированно
// становится одним argv-токеном.
func quoteShlex(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// QuoteShlex возвращает s в форме, которую shlex.Split интерпретирует как
// один argv-токон (см. quoteShlex). Экспортирован для переиспользования в
// modulerunner.RunHook (шаблоны attach:/detach:).
func QuoteShlex(s string) string {
	return quoteShlex(s)
}

// envMapFn возвращает map переменных окружения процесса из os.Environ().
// Package-level var — позволяет заменить реализацию для кэширования
// (HTTP server) или тестирования (фикс #43).
var envMapFn = defaultEnvMap

// defaultEnvMap читает os.Environ() при каждом вызове.
// Для CLI (один Render за запуск) — незаметно. Для HTTP server —
// замените envMapFn на кэшированную реализацию.
func defaultEnvMap() map[string]string {
	m := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.Index(kv, "="); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// normalizeBool приводит строковое значение bool-параметра к каноническому
// виду для корректной работы с {{if .bool}} в text/template, где любая
// непустая строка считается truthy. falsy-значения ("false", "0", "no", "")
// приводятся к "" (falsy), остальные — к "true". Сравнение регистронезависимое.
func normalizeBool(val string) string {
	switch strings.ToLower(val) {
	case "false", "0", "no", "":
		return ""
	default:
		return "true"
	}
}
