package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tivjnb/futi/internal/execmodule"
)

// shellSafe сообщает, что токен можно печатать без кавычек: он непустой и
// состоит только из символов, не имеющих особого смысла для shell.
func shellSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("_@%+=:,./-", r):
		default:
			return false
		}
	}
	return true
}

// needsANSIC сообщает, что токен содержит управляющие, непечатаемые руны или
// невалидный UTF-8: такой текст нельзя выводить в терминал как есть.
func needsANSIC(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size == 1) || !unicode.IsPrint(r) {
			return true
		}
		i += size
	}
	return false
}

// quoteANSIC печатает токен в форме $'…' (bash/zsh): обратный слэш и
// одинарная кавычка экранируются, управляющие байты — \xHH, непечатаемые
// руны — \uHHHH или \UHHHHHHHH. Копипаст даёт тот же argv.
func quoteANSIC(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, "\\x%02x", s[i])
		case r == '\\' || r == '\'':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x80 && !unicode.IsPrint(r):
			fmt.Fprintf(&b, "\\x%02x", r)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xffff:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			fmt.Fprintf(&b, "\\U%08x", r)
		}
		i += size
	}
	b.WriteByte('\'')
	return b.String()
}

// shellJoin склеивает argv в одну строку, которую можно вставить в shell
// (bash/zsh): безопасные токены печатаются как есть, остальные (в том числе
// пустые) заключаются в кавычки через execmodule.QuoteShlex, а токены с
// управляющими символами — в форме $'…'. В кавычки берутся также argv[0] с
// «=» (иначе это присваивание) и токен, начинающийся с «=» (zsh раскрывает
// =cmd).
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		switch {
		case needsANSIC(a):
			parts[i] = quoteANSIC(a)
		case shellSafe(a) && !strings.HasPrefix(a, "=") && !(i == 0 && strings.Contains(a, "=")):
			parts[i] = a
		default:
			parts[i] = execmodule.QuoteShlex(a)
		}
	}
	return strings.Join(parts, " ")
}
