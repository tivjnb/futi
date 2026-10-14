package modulecache

import (
	"testing"
	"time"
)

// TestAlwaysPolicyShouldRefreshAlwaysTrue проверяет, что AlwaysPolicy
// всегда возвращает true.
func TestAlwaysPolicyShouldRefreshAlwaysTrue(t *testing.T) {
	p := AlwaysPolicy{}
	if !p.ShouldRefresh(cacheFile{}) {
		t.Fatal("expected true for empty state")
	}
	if !p.ShouldRefresh(cacheFile{LastRefreshTime: time.Now()}) {
		t.Fatal("expected true for fresh state")
	}
}

// TestForceOnlyPolicyShouldRefreshAlwaysFalse проверяет, что ForceOnlyPolicy
// всегда возвращает false.
func TestForceOnlyPolicyShouldRefreshAlwaysFalse(t *testing.T) {
	p := ForceOnlyPolicy{}
	if p.ShouldRefresh(cacheFile{}) {
		t.Fatal("expected false for empty state")
	}
	if p.ShouldRefresh(cacheFile{LastRefreshTime: time.Now()}) {
		t.Fatal("expected false for fresh state")
	}
}

// TestEveryTMinPolicyShouldRefresh проверяет, что EveryTMinPolicy возвращает
// true если прошло больше T времени, false если меньше.
func TestEveryTMinPolicyShouldRefresh(t *testing.T) {
	p := EveryTMinPolicy{T: 30 * time.Minute}

	// LastRefreshTime давно — нужен refresh
	old := cacheFile{LastRefreshTime: time.Now().Add(-1 * time.Hour)}
	if !p.ShouldRefresh(old) {
		t.Fatal("expected true for state older than T")
	}

	// LastRefreshTime недавно — refresh не нужен
	fresh := cacheFile{LastRefreshTime: time.Now().Add(-5 * time.Minute)}
	if p.ShouldRefresh(fresh) {
		t.Fatal("expected false for state newer than T")
	}
}

// TestEveryTMinPolicyEmptyLastRefreshTime проверяет, что пустой LastRefreshTime
// (кэш никогда не обновлялся) → true.
func TestEveryTMinPolicyEmptyLastRefreshTime(t *testing.T) {
	p := EveryTMinPolicy{T: 30 * time.Minute}
	if !p.ShouldRefresh(cacheFile{}) {
		t.Fatal("expected true for empty LastRefreshTime (never refreshed)")
	}
}

// TestNewPolicyFromConfigEveryTime проверяет, что "every_time" → AlwaysPolicy.
func TestNewPolicyFromConfigEveryTime(t *testing.T) {
	p, err := NewPolicyFromConfig("every_time", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(AlwaysPolicy); !ok {
		t.Fatalf("expected AlwaysPolicy, got %T", p)
	}
}

// TestNewPolicyFromConfigEmptyStringDefaultsToEveryTime проверяет, что пустой
// refresh → AlwaysPolicy (default).
func TestNewPolicyFromConfigEmptyStringDefaultsToEveryTime(t *testing.T) {
	p, err := NewPolicyFromConfig("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(AlwaysPolicy); !ok {
		t.Fatalf("expected AlwaysPolicy for empty refresh, got %T", p)
	}
}

// TestNewPolicyFromConfigForceOnly проверяет, что "force_only" → ForceOnlyPolicy.
func TestNewPolicyFromConfigForceOnly(t *testing.T) {
	p, err := NewPolicyFromConfig("force_only", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(ForceOnlyPolicy); !ok {
		t.Fatalf("expected ForceOnlyPolicy, got %T", p)
	}
}

// TestNewPolicyFromConfigEveryTMinValid проверяет, что "every_t_min" с валидным
// time → EveryTMinPolicy.
func TestNewPolicyFromConfigEveryTMinValid(t *testing.T) {
	p, err := NewPolicyFromConfig("every_t_min", "30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	etm, ok := p.(EveryTMinPolicy)
	if !ok {
		t.Fatalf("expected EveryTMinPolicy, got %T", p)
	}
	if etm.T != 30*time.Minute {
		t.Fatalf("expected T=30m, got %v", etm.T)
	}
}

// TestNewPolicyFromConfigEveryTMinWithDays проверяет, что "every_t_min" с
// временем в днях работает.
func TestNewPolicyFromConfigEveryTMinWithDays(t *testing.T) {
	p, err := NewPolicyFromConfig("every_t_min", "1d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	etm, ok := p.(EveryTMinPolicy)
	if !ok {
		t.Fatalf("expected EveryTMinPolicy, got %T", p)
	}
	if etm.T != 24*time.Hour {
		t.Fatalf("expected T=24h, got %v", etm.T)
	}
}

// TestNewPolicyFromConfigEveryTMinMissingTime проверяет, что "every_t_min"
// без time → error.
func TestNewPolicyFromConfigEveryTMinMissingTime(t *testing.T) {
	_, err := NewPolicyFromConfig("every_t_min", "")
	if err == nil {
		t.Fatal("expected error for every_t_min without time, got nil")
	}
}

// TestNewPolicyFromConfigEveryTMinInvalidTime проверяет, что "every_t_min"
// с невалидным time → error.
func TestNewPolicyFromConfigEveryTMinInvalidTime(t *testing.T) {
	_, err := NewPolicyFromConfig("every_t_min", "abc")
	if err == nil {
		t.Fatal("expected error for invalid time, got nil")
	}
}

// TestNewPolicyFromConfigInvalidRefresh проверяет, что невалидный refresh → error.
func TestNewPolicyFromConfigInvalidRefresh(t *testing.T) {
	_, err := NewPolicyFromConfig("invalid_value", "")
	if err == nil {
		t.Fatal("expected error for invalid refresh, got nil")
	}
}

// TestEveryTMinPolicyClocksBackRefreshes проверяет, что при отрицательном
// time.Since (системные часы переведены назад — NTP sync) ShouldRefresh
// возвращает true. Без фикса: negative > T → false → кэш не обновится
// (REVIEW #31). Edge case реальный: NTP может перевести часы назад.
func TestEveryTMinPolicyClocksBackRefreshes(t *testing.T) {
	p := EveryTMinPolicy{T: 30 * time.Minute}
	// LastRefreshTime в будущем относительно now — означает часы пошли назад.
	future := cacheFile{LastRefreshTime: time.Now().Add(1 * time.Hour)}
	if !p.ShouldRefresh(future) {
		t.Fatal("expected true when LastRefreshTime is in the future (clocks went back)")
	}
}

// TestNewPolicyFromConfigEveryTMinTooLarge проверяет, что T > 365 дней
// отклоняется (фикс #38 — экстремальные T эквивалентны force_only без явного
// заявления).
func TestNewPolicyFromConfigEveryTMinTooLarge(t *testing.T) {
	_, err := NewPolicyFromConfig("every_t_min", "366d")
	if err == nil {
		t.Fatal("expected error for T > 365 days, got nil")
	}
}

// TestNewPolicyFromConfigEveryTMinMaxAllowed проверяет, что 365d принимается.
func TestNewPolicyFromConfigEveryTMinMaxAllowed(t *testing.T) {
	p, err := NewPolicyFromConfig("every_t_min", "365d")
	if err != nil {
		t.Fatalf("365d should be accepted, got: %v", err)
	}
	if _, ok := p.(EveryTMinPolicy); !ok {
		t.Fatalf("expected EveryTMinPolicy, got %T", p)
	}
}
