// Package core — общее ядро futi: параметры окружения и (далее) Runtime.
package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// DefaultModulesDir — глобальный каталог установки: FUTI_MODULES_DIR или
// config.DefaultModulesDir(). Единственное место чтения FUTI_MODULES_DIR.
func DefaultModulesDir() (string, error) {
	if dir := os.Getenv("FUTI_MODULES_DIR"); dir != "" {
		return dir, nil
	}
	return config.DefaultModulesDir()
}

// LoadDefaultConfig загружает config.yaml futi по пути по умолчанию;
// отсутствующий файл — (nil, nil).
func LoadDefaultConfig() (*config.Config, error) {
	path, err := config.DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return config.Load(path)
}

// Options — входные данные Runtime: пути и обработчик предупреждений.
type Options struct {
	ConfigPath   string           // "" = без конфига
	ModulesDir   string           // глобальный каталог (нужен, когда в конфиге нет sources)
	LocalDir     string           // "" = без локальных модулей; CLI — cwd
	CachePath    string           // "" = MemoryStorage (политика — из конфига)
	CachePathErr error            // ошибка вычисления пути кэша: MemoryStorage + AlwaysPolicy + предупреждение
	Warn         func(msg string) // nil = предупреждения отбрасываются

	loader modulerunner.ModuleLoader // nil = Runtime.Cache(); шов для тестов
}

// DefaultOptions — единственное чтение окружения для Runtime CLI.
func DefaultOptions() (Options, error) {
	var opts Options

	configPath, err := config.DefaultConfigPath()
	if err != nil {
		return Options{}, fmt.Errorf("config path: %w", err)
	}
	opts.ConfigPath = configPath

	modulesDir, err := DefaultModulesDir()
	if err != nil {
		return Options{}, fmt.Errorf("modules dir: %w", err)
	}
	opts.ModulesDir = modulesDir

	if cwd, err := os.Getwd(); err == nil {
		opts.LocalDir = cwd
	} else {
		opts.LocalDir = "."
	}

	if dir, err := os.UserCacheDir(); err == nil {
		opts.CachePath = filepath.Join(dir, "futi", "modulecache.gob")
	} else {
		opts.CachePathErr = fmt.Errorf("cache path: get cache dir: %w", err)
	}

	return opts, nil
}
