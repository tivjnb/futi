package modulecache

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDuration расширяет time.ParseDuration поддержкой суффикса "d" (дни).
//
// Формат: последовательность <число><единица>, где единица ∈ {d, h, m, s, ms, us, µs, ns}.
// Компоненты суммируются. Примеры: "30m", "2h", "1d", "1d4h", "1d2h30m", "90s".
//
// Если строка не содержит "d" — делегирует в time.ParseDuration (включая все
// стандартные единицы и их комбинации).
//
// Отрицательные значения не поддерживаются — интервал обновления не может быть
// отрицательным. Возвращает ошибку при невалидном формате.
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("modulecache: empty duration")
	}

	// Если нет "d" — делегируем в time.ParseDuration.
	if !strings.Contains(s, "d") {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("modulecache: parse duration %q: %w", s, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("modulecache: duration %q must be > 0", s)
		}
		return d, nil
	}

	// Строка содержит "d". Разделяем на ведущие <число>d и остаток.
	// Ищем позицию первого "d".
	dIdx := strings.Index(s, "d")
	daysPart := s[:dIdx]
	rest := s[dIdx+1:]

	// Парсим дни. Должно быть целое число > 0. strconv.Atoi требует, чтобы
	// вся строка была числом — это ловит "1.5d" (Atoi("1.5") → error).
	days, err := strconv.Atoi(daysPart)
	if err != nil {
		return 0, fmt.Errorf("modulecache: parse days %q: %w", s, err)
	}
	if days <= 0 {
		return 0, fmt.Errorf("modulecache: days in %q must be > 0", s)
	}

	total := time.Duration(days) * 24 * time.Hour

	// Если есть остаток — парсим через time.ParseDuration.
	if rest != "" {
		restDur, err := time.ParseDuration(rest)
		if err != nil {
			return 0, fmt.Errorf("modulecache: parse duration %q: %w", s, err)
		}
		if restDur <= 0 {
			return 0, fmt.Errorf("modulecache: duration %q must be > 0", s)
		}
		total += restDur
	}

	return total, nil
}
