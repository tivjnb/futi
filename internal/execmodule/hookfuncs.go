package execmodule

import (
	"fmt"
	"strings"
	"text/template"
)

// TemplateError — ошибка парсинга или выполнения шаблона хука (attach/detach).
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

// TemplateFuncs — набор функций шаблонов хуков attach/detach (quote): им
// рендерятся хуки, и по нему же Validate проверяет их синтаксис. Хуки
// переходят на общий язык cmd отдельной задачей.
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{"quote": quoteArg}
}

// quoteArg возвращает s как одно слово POSIX shell: хуки исполняются через
// sh -c, поэтому значение должно остаться одним токеном при любых пробелах,
// кавычках и метасимволах. Обёртка в одинарные кавычки; каждая внутренняя
// одинарная кавычка заменяется последовательностью (закрывающая кавычка,
// экранированная кавычка, открывающая кавычка):
//
//	'\''
func quoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
