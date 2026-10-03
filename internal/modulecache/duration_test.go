package modulecache

import (
	"testing"
	"time"
)

// TestParseDurationDelegatesToTimeParseDuration проверяет, что строки без "d"
// делегируются в time.ParseDuration.
func TestParseDurationDelegatesToTimeParseDuration(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"2h", 2 * time.Hour},
		{"90s", 90 * time.Second},
		{"500ms", 500 * time.Millisecond},
		{"1h30m", 90 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseDurationSupportsDays проверяет, что "d" (дни) парсится корректно.
func TestParseDurationSupportsDays(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"1d", 24 * time.Hour},
		{"2d", 48 * time.Hour},
		{"1d4h", 28 * time.Hour},
		{"1d2h30m", 26*time.Hour + 30*time.Minute},
		{"3d12h", 84 * time.Hour},
		{"1d0h", 24 * time.Hour}, // #32: zero-duration rest should be accepted
		{"1d0m", 24 * time.Hour}, // #32: same for 0m
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseDurationEmptyReturnsError проверяет, что пустая строка — ошибка.
func TestParseDurationEmptyReturnsError(t *testing.T) {
	_, err := parseDuration("")
	if err == nil {
		t.Fatal("expected error for empty string, got nil")
	}
}

// TestParseDurationInvalidReturnsError проверяет, что невалидный формат — ошибка.
func TestParseDurationInvalidReturnsError(t *testing.T) {
	cases := []string{
		"abc",
		"5x",
		"5",
		"d",
		"1d2",   // нет единицы у последнего числа
		"1.5d",  // не поддерживаем дробные дни (time.ParseDuration не парсит дробные единицы без точки)
		"-5m",   // отрицательные не имеют смысла для интервала обновления
		"1d-2h", // отрицательный остаток
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			_, err := parseDuration(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
		})
	}
}

// TestParseDurationDayWordGivesClearError проверяет, что "1day" даёт понятное
// сообщение об ошибке (фикс #33 — strings.Contains(s, "d") ловил "1day").
func TestParseDurationDayWordGivesClearError(t *testing.T) {
	_, err := parseDuration("1day")
	if err == nil {
		t.Fatal("expected error for '1day'")
	}
}

// TestParseDurationOverflowRejected проверяет, что экстремальные значения
// days-компоненты отклоняются ДО умножения (REV-007): раньше молча
// переполнялся int64 — получался крошечный или отрицательный интервал,
// обходящий лимит 365 дней в policy (ShouldRefresh = всегда true).
func TestParseDurationOverflowRejected(t *testing.T) {
	// maxDays = math.MaxInt64 / int64(24h) = 106751.
	cases := []string{
		"107000d",                  // чуть больше максимума
		"1000000000d",              // 10^9 — раньше давало отрицательный интервал
		"1000000000000000000d",     // 10^18 — раньше переполнение в крошечное значение
		"9223372036854775807d",     // MaxInt64 дней
		"106751d24h",               // days валиден, но сложение с rest переполняет
		"10000000000000000000000d", // больше MaxInt64 — Atoi должен отклонить
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			d, err := parseDuration(tc)
			if err == nil {
				t.Fatalf("parseDuration(%q) = %v, want error", tc, d)
			}
			if d != 0 {
				t.Fatalf("parseDuration(%q) returned nonzero duration with error", tc)
			}
		})
	}
}

// TestParseDurationMaxDaysAccepted проверяет границу: максимум days-компоненты
// без переполнения принимается (ревью REV-007 — лимит 365 дней остаётся в
// policy, parser валидирует только математику int64).
func TestParseDurationMaxDaysAccepted(t *testing.T) {
	d, err := parseDuration("106751d")
	if err != nil {
		t.Fatalf("parseDuration(\"106751d\") unexpected error: %v", err)
	}
	if d <= 0 {
		t.Fatalf("parseDuration(\"106751d\") = %v, want > 0", d)
	}

	// Максимум + rest в пределах int64 тоже принимается.
	d, err = parseDuration("106751d23h")
	if err != nil {
		t.Fatalf("parseDuration(\"106751d23h\") unexpected error: %v", err)
	}
	if d <= 0 {
		t.Fatalf("parseDuration(\"106751d23h\") = %v, want > 0", d)
	}
}
