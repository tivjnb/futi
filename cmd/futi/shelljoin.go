package main

import (
	"strings"

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

// shellJoin склеивает argv в одну строку, которую можно вставить в shell:
// безопасные токены печатаются как есть, остальные (в том числе пустые)
// заключаются в кавычки через execmodule.QuoteShlex.
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if shellSafe(a) {
			parts[i] = a
		} else {
			parts[i] = execmodule.QuoteShlex(a)
		}
	}
	return strings.Join(parts, " ")
}
