package modulecache

import (
	"fmt"
	"time"
)

// RefreshPolicy — стратегия обновления кэша.
//
// Реализации:
//   - AlwaysPolicy — обновлять при каждом запуске (default).
//   - EveryTMinPolicy{T} — обновлять раз в T времени.
//   - ForceOnlyPolicy — никогда автоматически, только futi cache refresh.
//
// Для будущего HTTP server (ROADMAP): TimerPolicy (раз в N минут) или
// OnDemandPolicy (по HTTP-ручке) без изменения Cache.
type RefreshPolicy interface {
	// ShouldRefresh возвращает true если кэш нужно обновить.
	// state — текущее состояние cacheFile (LastRefreshTime, Entries).
	ShouldRefresh(state cacheFile) bool
}

// AlwaysPolicy — обновлять при каждом запуске CLI.
type AlwaysPolicy struct{}

// ShouldRefresh всегда возвращает true.
func (AlwaysPolicy) ShouldRefresh(_ cacheFile) bool { return true }

// EveryTMinPolicy — обновлять раз в T времени.
// Использует LastRefreshTime из cacheFile.
type EveryTMinPolicy struct {
	T time.Duration
}

// ShouldRefresh возвращает true если time.Since(state.LastRefreshTime) > T.
// Пустой LastRefreshTime (кэш никогда не обновлялся) → true.
// Отрицательный elapsed (системные часы переведены назад — NTP sync) → true:
// кэш может быть stale относительно нового времени, безопаснее refresh.
func (p EveryTMinPolicy) ShouldRefresh(state cacheFile) bool {
	if state.LastRefreshTime.IsZero() {
		return true
	}
	elapsed := time.Since(state.LastRefreshTime)
	if elapsed < 0 {
		// Часы переведены назад (NTP sync) — refresh чтобы быть безопасным.
		return true
	}
	return elapsed > p.T
}

// ForceOnlyPolicy — никогда автоматически. CLI молча использует кэш как есть.
type ForceOnlyPolicy struct{}

// ShouldRefresh всегда возвращает false.
func (ForceOnlyPolicy) ShouldRefresh(_ cacheFile) bool { return false }

// NewPolicyFromConfig создаёт RefreshPolicy по config-значениям.
//
// Параметры:
//   - refresh: "every_time" (default) | "every_t_min" | "force_only"
//   - timeStr: длительность для "every_t_min" (например "30m", "1d4h")
//
// Возвращает error при невалидных значениях. Вызывающий (main.go) при ошибке
// fallback на AlwaysPolicy.
func NewPolicyFromConfig(refresh, timeStr string) (RefreshPolicy, error) {
	switch refresh {
	case "every_time", "":
		return AlwaysPolicy{}, nil
	case "every_t_min":
		if timeStr == "" {
			return nil, fmt.Errorf("cache: time is required for every_t_min")
		}
		d, err := parseDuration(timeStr)
		if err != nil {
			return nil, fmt.Errorf("cache: invalid time %q: %w", timeStr, err)
		}
		return EveryTMinPolicy{T: d}, nil
	case "force_only":
		return ForceOnlyPolicy{}, nil
	default:
		return nil, fmt.Errorf("cache: invalid refresh %q (valid: every_time, every_t_min, force_only)", refresh)
	}
}
