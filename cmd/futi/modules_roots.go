package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/termtext"
)

// moduleRoot — корень, в котором ищутся установленные модули: каталог и
// namespace-префикс, под которым discovery резолвит его модули.
type moduleRoot struct {
	Dir    string
	Prefix string
}

// globalModulesDir — глобальный каталог установки: FUTI_MODULES_DIR или
// каталог по умолчанию. Единственное место вычисления этого корня.
func globalModulesDir() (string, error) {
	if dir := os.Getenv("FUTI_MODULES_DIR"); dir != "" {
		return dir, nil
	}
	return config.DefaultModulesDir()
}

// loadFutiConfig загружает config.yaml futi; отсутствующий файл — (nil, nil).
func loadFutiConfig() (*config.Config, error) {
	path, err := config.DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return config.Load(path)
}

// readModuleRoots — корни для чтения (list, update, uninstall): каждый source
// из config.yaml со своим префиксом, глобальный каталог установки и ./.futi
// (если есть) без префикса. Корни с одним реальным путём сводятся к первому,
// поэтому глобальный каталог, совпадающий с source, получает префикс source.
// Невалидный config.yaml — ошибка: иначе модули его sources молча пропали бы
// из list, а uninstall отказал бы «вне корней».
func readModuleRoots() ([]moduleRoot, error) {
	cfg, err := loadFutiConfig()
	if err != nil {
		return nil, err
	}
	var roots []moduleRoot
	if cfg != nil {
		for _, sc := range cfg.Sources {
			roots = append(roots, moduleRoot{Dir: sc.Path, Prefix: sc.FullPrefix()})
		}
	}
	globalDir, err := globalModulesDir()
	if err != nil {
		return nil, err
	}
	roots = append(roots, moduleRoot{Dir: globalDir})
	if info, err := os.Stat(".futi"); err == nil && info.IsDir() {
		if abs, err := filepath.Abs(".futi"); err == nil {
			roots = append(roots, moduleRoot{Dir: abs})
		}
	}

	seen := make(map[string]bool, len(roots))
	unique := roots[:0]
	for _, r := range roots {
		real := normalizePath(r.Dir)
		if seen[real] {
			continue
		}
		seen[real] = true
		unique = append(unique, r)
	}
	return unique, nil
}

// rootDirs — каталоги корней.
func rootDirs(roots []moduleRoot) []string {
	dirs := make([]string, 0, len(roots))
	for _, r := range roots {
		dirs = append(dirs, r.Dir)
	}
	return dirs
}

// prefixedName — имя модуля с относительным dot-именем rel в корне с
// префиксом prefix, как его резолвит discovery.
func prefixedName(prefix, rel string) string {
	if prefix == "" {
		return rel
	}
	return prefix + "." + rel
}

// reportSourcesVisibility сообщает, как discovery увидит модуль name (dot-имя
// внутри глобального каталога globalDir), если в config.yaml заданы sources:
// при них глобальный каталог сам по себе не читается. Каталог не входит в
// sources — предупреждение в stderr; входит с префиксом — полное имя в w.
func reportSourcesVisibility(w io.Writer, cfg *config.Config, globalDir, name string) {
	if cfg == nil || len(cfg.Sources) == 0 {
		return
	}
	realGlobal := normalizePath(globalDir)
	for _, sc := range cfg.Sources {
		if normalizePath(sc.Path) != realGlobal {
			continue
		}
		if prefix := sc.FullPrefix(); prefix != "" {
			fmt.Fprintf(w, "Module is available as %s\n", termtext.Line(prefixedName(prefix, name)))
		}
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: %s is not listed in config.yaml sources; add it there to use the module\n", termtext.Line(globalDir))
}

// stripRootPrefix возвращает имя модуля name относительно корня с префиксом
// prefix; ok == false, если name не из этого корня.
func stripRootPrefix(prefix, name string) (string, bool) {
	if prefix == "" {
		return name, true
	}
	rest, ok := strings.CutPrefix(name, prefix+".")
	return rest, ok && rest != ""
}
