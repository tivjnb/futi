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
		"1d2",    // нет единицы у последнего числа
		"1.5d",   // не поддерживаем дробные дни (time.ParseDuration не парсит дробные единицы без точки)
		"-5m",    // отрицательные не имеют смысла для интервала обновления
		"1d-2h",  // отрицательный остаток
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
