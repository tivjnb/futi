package modulecache

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"
)

// daysRe matches leading digits followed by "d" as a days unit (e.g. "1d", "3d12h").
// #33: не ловит "1day" — "d" должен быть unit-суффиксом, а не частью слова.
var daysRe = regexp.MustCompile(`^(\d+)d`)

// parseDuration расширяет time.ParseDuration поддержкой суффикса "d" (дни).
//
// Формат: последовательность <число><единица>, где единица ∈ {d, h, m, s, ms, us, µs, ns}.
// Компоненты суммируются. Примеры: "30m", "2h", "1d", "1d4h", "1d2h30m", "90s".
//
// Если строка не содержит "d" как unit (digits + "d") — делегирует в time.ParseDuration.
// #32: нулевые rest-компоненты (1d0h) принимаются.
// #33: "1day" не matches daysRe → делегирует в time.ParseDuration → понятная ошибка.
//
// Отрицательные значения не поддерживаются — интервал обновления не может быть
// отрицательным. Возвращает ошибку при невалидном формате.
// maxDays — максимум days-компоненты без переполнения int64 наносекунд.
// time.Duration хранит int64 ns: максимум ~2562047h47m ≈ 106751d.
// #REV-007: большие значения раньше молча переполнялись (крошечный или
// отрицательный интервал, обходящий лимит every_t_min в policy).
const maxDays = int(math.MaxInt64 / int64(24*time.Hour))

func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("modulecache: empty duration")
	}

	// Проверяем, начинается ли строка с <digits>d.
	m := daysRe.FindStringSubmatch(s)
	if m == nil {
		// Нет days-компонента — делегируем в time.ParseDuration.
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("modulecache: parse duration %q: %w", s, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("modulecache: duration %q must be > 0", s)
		}
		return d, nil
	}

	daysPart := m[1]
	rest := s[len(m[0]):]

	days, err := strconv.Atoi(daysPart)
	if err != nil {
		return 0, fmt.Errorf("modulecache: parse days %q: %w", s, err)
	}
	// REV-007: проверяем ДО умножения — time.Duration(days)*24h переполняет
	// int64 для days > maxDays (Atoi принимает до 2^63-1).
	if days > maxDays {
		return 0, fmt.Errorf("modulecache: days in %q too large (max %dd)", s, maxDays)
	}

	total := time.Duration(days) * 24 * time.Hour

	// Если есть остаток — парсим через time.ParseDuration.
	// #32: нулевой rest (0h, 0m) принимается, если итог > 0 (проверка ниже).
	if rest != "" {
		restDur, err := time.ParseDuration(rest)
		if err != nil {
			return 0, fmt.Errorf("modulecache: parse duration %q: %w", s, err)
		}
		if restDur < 0 {
			return 0, fmt.Errorf("modulecache: duration %q must be > 0", s)
		}
		total += restDur
		// REV-007: сложение с rest могло переполнить int64 — отрицательный
		// итог отклоняем (ноль ловит проверка ниже).
		if total < 0 {
			return 0, fmt.Errorf("modulecache: duration %q too large", s)
		}
	}

	// audit F42: ноль проверяем по сумме компонент: "0d12h" допустимо,
	// "0d" и "0d0h" — нет.
	if total <= 0 {
		return 0, fmt.Errorf("modulecache: duration %q must be > 0", s)
	}

	return total, nil
}
