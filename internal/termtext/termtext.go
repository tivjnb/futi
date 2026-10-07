// Package termtext фильтрует управляющие символы в тексте из YAML и lockfile
// перед выводом в терминал: ESC/OSC/CSI-последовательности из чужого модуля
// не должны доходить до терминала пользователя.
package termtext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// filter строит строку, пропуская каждую руну через keep (-1 — удалить).
// Невалидные байты UTF-8 (например, одиночный 0x9B — C1 CSI для 8-битных
// терминалов) удаляются: strings.Map подменил бы их на U+FFFD.
func filter(s string, keep func(rune) rune) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if k := keep(r); k >= 0 {
			b.WriteRune(k)
		}
	}
	return b.String()
}

// isBidiControl сообщает, что r — bidi-управляющий символ: им можно визуально
// переставить текст в терминале (trojan source). Прочие Cf (например ZWJ в
// emoji) не трогаем.
func isBidiControl(r rune) bool {
	return r == 0x061C || r == 0x200E || r == 0x200F ||
		(r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// Line подготавливает однострочный текст: \t, \n, \r заменяются пробелом,
// прочие управляющие руны (C0, DEL, C1) и bidi-управляющие удаляются, края обрезаются.
func Line(s string) string {
	return strings.TrimSpace(filter(s, func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r) || isBidiControl(r):
			return -1
		}
		return r
	}))
}

// Block подготавливает многострочный текст: \n и \t сохраняются,
// \r, прочие управляющие руны и bidi-управляющие удаляются.
func Block(s string) string {
	return filter(s, func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r) || isBidiControl(r):
			return -1
		}
		return r
	})
}
