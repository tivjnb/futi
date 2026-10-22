package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulecache"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// Runtime — загруженный конфиг и глобальный source; кэш и итоговый source
// создаются лениво, один раз. Безопасен для конкурентного использования.
type Runtime struct {
	opts   Options
	cfg    *config.Config
	global discovery.Source

	cacheOnce sync.Once
	cache     *modulecache.Cache

	sourceOnce sync.Once
	source     discovery.Source
	sourceErr  error
}

// NewRuntime загружает конфиг (если задан ConfigPath) и создаёт глобальный source.
// Локальные файлы и кэш не затрагиваются до первого Source()/Cache().
func NewRuntime(opts Options) (*Runtime, error) {
	r := &Runtime{opts: opts}

	if opts.ConfigPath != "" {
		cfg, err := config.Load(opts.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		r.cfg = cfg
	}

	if r.cfg != nil && len(r.cfg.Sources) > 0 {
		r.global = discovery.NewCompositeSource(r.cfg)
		return r, nil
	}
	if opts.ModulesDir == "" {
		return nil, errors.New("modules dir: not set")
	}
	r.global = discovery.NewGlobalSource(opts.ModulesDir)
	return r, nil
}

// Config возвращает загруженный конфиг; nil — конфига нет.
func (r *Runtime) Config() *config.Config { return r.cfg }

// GlobalSource возвращает глобальный source (без локальных слоёв).
func (r *Runtime) GlobalSource() discovery.Source { return r.global }

// CachePath — путь gob-файла кэша; "" при CachePathErr или пустом CachePath.
func (r *Runtime) CachePath() string {
	if r.opts.CachePathErr != nil {
		return ""
	}
	return r.opts.CachePath
}

// ModulesDir возвращает глобальный каталог модулей из Options.
func (r *Runtime) ModulesDir() string { return r.opts.ModulesDir }

// LocalDir возвращает каталог локальных модулей из Options.
func (r *Runtime) LocalDir() string { return r.opts.LocalDir }

func (r *Runtime) warn(msg string) {
	if r.opts.Warn != nil {
		r.opts.Warn(msg)
	}
}

// Cache лениво создаёт кэш модулей. Ошибка пути кэша или политики из конфига
// не фатальна: предупреждение и откат на MemoryStorage / AlwaysPolicy.
func (r *Runtime) Cache() *modulecache.Cache {
	r.cacheOnce.Do(func() {
		if r.opts.CachePathErr != nil {
			r.warn(fmt.Sprintf("cache: %v (using in-memory only)", r.opts.CachePathErr))
			r.cache = modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
			return
		}

		var storage modulecache.Storage = modulecache.MemoryStorage{}
		if r.opts.CachePath != "" {
			storage = modulecache.NewDiskStorage(r.opts.CachePath)
		}

		policy := modulecache.RefreshPolicy(modulecache.AlwaysPolicy{})
		if r.cfg != nil {
			p, err := modulecache.NewPolicyFromConfig(r.cfg.Cache.Refresh, r.cfg.Cache.Time)
			if err != nil {
				r.warn(fmt.Sprintf("cache config: %v (using default every_time)", err))
			} else {
				policy = p
			}
		}
		r.cache = modulecache.New(storage, policy)
	})
	return r.cache
}

// loader returns the module loader: the injected one, else the cache.
func (r *Runtime) loader() modulerunner.ModuleLoader {
	if r.opts.loader != nil {
		return r.opts.loader
	}
	return r.Cache()
}

// Start запускает кэш по глобальному source (обновление по политике).
func (r *Runtime) Start() error {
	return r.Cache().Start(r.global)
}

// Source лениво создаёт итоговый source: локальные модули (LocalDir/.futi,
// LocalDir/futi.yaml) поверх глобального. Результат и ошибка запоминаются.
func (r *Runtime) Source() (discovery.Source, error) {
	r.sourceOnce.Do(func() {
		r.source, r.sourceErr = r.buildSource()
	})
	return r.source, r.sourceErr
}

func (r *Runtime) buildSource() (discovery.Source, error) {
	if r.opts.LocalDir == "" {
		return r.global, nil
	}
	local, err := r.localSource()
	if err != nil {
		return nil, err
	}
	if local != nil {
		return discovery.NewLayeredSource(local, r.global), nil
	}
	return r.global, nil
}

// localSource проверяет LocalDir на локальные модули:
//   - .futi/ — каталог модулей (GlobalSource)
//   - futi.yaml — одиночный файл модуля (FileSource, имя из поля name)
//
// Возвращает (nil, nil), если локальных модулей нет. При наличии обоих
// .futi/ имеет приоритет над futi.yaml. futi.yaml загружается loader-ом
// по абсолютному пути, чтобы ключ кэша совпадал с тем, что использует Run.
// Невалидный futi.yaml — ошибка (ранняя диагностика вместо "module not found").
func (r *Runtime) localSource() (discovery.Source, error) {
	var layers []discovery.Source

	dotFuti := filepath.Join(r.opts.LocalDir, ".futi")
	if info, err := os.Stat(dotFuti); err == nil && info.IsDir() {
		abs, _ := filepath.Abs(dotFuti)
		layers = append(layers, discovery.NewGlobalSource(abs))
	}

	futiYaml := filepath.Join(r.opts.LocalDir, "futi.yaml")
	if _, err := os.Stat(futiYaml); err == nil {
		abs, _ := filepath.Abs(futiYaml)
		absRoot, _ := filepath.Abs(r.opts.LocalDir)
		mod, err := modulerunner.LoadModule(r.loader(), abs, absRoot)
		if err != nil {
			return nil, fmt.Errorf("local source: %w", err)
		}
		layers = append(layers, discovery.NewFileSource(abs, absRoot, execmodule.EffectiveName(mod.Name, abs)))
	}

	switch len(layers) {
	case 0:
		return nil, nil
	case 1:
		return layers[0], nil
	}
	return discovery.NewLayeredSource(layers...), nil
}
