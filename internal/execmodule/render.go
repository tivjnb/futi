package execmodule

import (
	"bytes"
	"fmt"
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

// Render подставляет значения параметров и args в cmd-шаблон.
// Использует Go text/template: {{.var}} для подстановки params,
// {{.args}} для строки args, {{index .args_list N}} для элемента массива args,
// {{if .bool}}...{{end}} для условных блоков.
func (c *Command) Render(params map[string]string, args string, argsList []string) (string, error) {
	tmpl, err := template.New("cmd").Option("missingkey=error").Parse(c.Cmd)
	if err != nil {
		return "", fmt.Errorf("execmodule: parse template %q: %w", c.Cmd, err)
	}

	data := make(map[string]interface{}, len(params)+2)
	for k, v := range params {
		data[k] = v
	}
	data["args"] = args
	if argsList == nil {
		argsList = []string{}
	}
	data["args_list"] = argsList

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execmodule: render template %q: %w", c.Cmd, err)
	}

	return buf.String(), nil
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
