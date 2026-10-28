package modules

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

// moduleRootsFrom — корни для чтения (list, update, uninstall): каждый source
// из config.yaml со своим префиксом, глобальный каталог установки и
// <localDir>/.futi (если есть) без префикса; при localDir == "" локальный
// корень пропускается. Корни с одним реальным путём сводятся к первому,
// поэтому глобальный каталог, совпадающий с source, получает префикс source.
func moduleRootsFrom(cfg *config.Config, globalDir, localDir string) []moduleRoot {
	var roots []moduleRoot
	if cfg != nil {
		for _, sc := range cfg.Sources {
			roots = append(roots, moduleRoot{Dir: sc.Path, Prefix: sc.FullPrefix()})
		}
	}
	roots = append(roots, moduleRoot{Dir: globalDir})
	if localDir != "" {
		local := filepath.Join(localDir, ".futi")
		if info, err := os.Stat(local); err == nil && info.IsDir() {
			if abs, err := filepath.Abs(local); err == nil {
				roots = append(roots, moduleRoot{Dir: abs})
			}
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
	return unique
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
// при них глобальный каталог сам по себе не читается. Каталог совпадает с
// source или лежит внутри него — полное имя в w (если оно отличается от
// name); не входит ни в один source — предупреждение в stderr.
func reportSourcesVisibility(w, errW io.Writer, cfg *config.Config, globalDir, name string) {
	if cfg == nil || len(cfg.Sources) == 0 {
		return
	}
	fullName, ok := visibleName(cfg.Sources, normalizePath(globalDir), name)
	if !ok {
		fmt.Fprintf(errW, "Warning: %s is not listed in config.yaml sources; add it there to use, update or uninstall the module by name\n", termtext.Line(globalDir))
		return
	}
	if fullName != name {
		fmt.Fprintf(w, "Module is available as %s\n", termtext.Line(fullName))
	}
}

// visibleName — имя, под которым discovery найдёт модуль name глобального
// каталога с реальным путём realGlobal: source с тем же путём важнее source,
// внутри которого каталог лежит (тогда к имени добавляется путь от source).
func visibleName(sources []config.SourceConfig, realGlobal, name string) (string, bool) {
	for _, sc := range sources {
		if normalizePath(sc.Path) == realGlobal {
			return prefixedName(sc.FullPrefix(), name), true
		}
	}
	for _, sc := range sources {
		realSource := normalizePath(sc.Path)
		if !strictlyInside(realSource, realGlobal) {
			continue
		}
		rel, err := filepath.Rel(realSource, realGlobal)
		if err != nil {
			continue
		}
		return prefixedName(sc.FullPrefix(), dotName(rel)+"."+name), true
	}
	return "", false
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
