package execmodule

import (
	"path/filepath"
	"strings"
)

// EffectiveName возвращает имя модуля для {{.module_name}} и
// FUTI_MODULE_NAME: поле name из манифеста, а если оно пусто — последний
// сегмент пути модуля. Для <x>/config.yaml это имя каталога <x>, для
// <x>.yaml — <x>.
func EffectiveName(rawName, manifestPath string) string {
	if rawName != "" {
		return rawName
	}
	if abs, err := filepath.Abs(manifestPath); err == nil {
		manifestPath = abs
	}
	base := filepath.Base(manifestPath)
	if base == "config.yaml" {
		return filepath.Base(filepath.Dir(manifestPath))
	}
	return strings.TrimSuffix(base, ".yaml")
}
