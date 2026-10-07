package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/modulecache"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// uninstallArgs — аргументы `futi modules uninstall`.
type uninstallArgs struct {
	name         string
	force        bool
	yes          bool
	skipDetach   bool
	strictDetach bool
}

// parseUninstallArgs разбирает аргументы uninstall.
func parseUninstallArgs(args []string) (*uninstallArgs, error) {
	cfg := &uninstallArgs{}
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--force":
			cfg.force = true
		case arg == "--yes" || arg == "-y":
			cfg.yes = true
		case arg == "--skip-detach":
			cfg.skipDetach = true
		case arg == "--strict-detach":
			cfg.strictDetach = true
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) == 0 {
		return nil, fmt.Errorf("usage: futi modules uninstall <name> [flags]")
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one module name, got %d", len(positional))
	}
	cfg.name = positional[0]
	// Имя проверяем до Resolve: "" / "." иначе резолвятся в корень каталога модулей,
	// а "a/b" обходит проверки пакета в Resolve.
	if err := validateResolveName(cfg.name); err != nil {
		return nil, err
	}
	return cfg, nil
}

// allowedUninstallRoots — каталоги, внутри которых разрешено удалять:
// глобальный + ./.futi (если существует).
func allowedUninstallRoots() ([]string, error) {
	globalDir := os.Getenv("FUTI_MODULES_DIR")
	if globalDir == "" {
		dir, err := config.DefaultModulesDir()
		if err != nil {
			return nil, err
		}
		globalDir = dir
	}
	roots := []string{globalDir}
	if info, err := os.Stat(".futi"); err == nil && info.IsDir() {
		if abs, err := filepath.Abs(".futi"); err == nil {
			roots = append(roots, abs)
		}
	}
	return roots, nil
}

// runModulesUninstall удаляет установленный модуль (спека §5.2).
// stdin — для интерактивного подтверждения (os.Stdin из main).
func runModulesUninstall(w io.Writer, stdin io.Reader, source discovery.Source, cache *modulecache.Cache, args []string) error {
	cfg, err := parseUninstallArgs(args)
	if err != nil {
		return err
	}

	manifestPath, moduleRoot, err := source.Resolve(cfg.name)
	if err != nil {
		return fmt.Errorf("modules uninstall: %w", err)
	}

	roots, err := allowedUninstallRoots()
	if err != nil {
		return fmt.Errorf("modules uninstall: %w", err)
	}

	// Цель удаления: пакет (каталог с config.yaml) или одиночный yaml-файл.
	// moduleRoot одиночного yaml — родительский каталог, удалять его нельзя.
	target := moduleRoot
	isPackage := filepath.Base(manifestPath) == "config.yaml"
	if !isPackage {
		target = manifestPath
	}
	if err := ensureWithinRoot(target, roots); err != nil {
		return fmt.Errorf("modules uninstall: %w", err)
	}

	// Sidecar лежит рядом с манифестом (для пакета = moduleRoot).
	metaPath := filepath.Join(filepath.Dir(manifestPath), metayaml.FileName)
	_, metaErr := os.Stat(metaPath)
	if metaErr != nil {
		if !cfg.force {
			return fmt.Errorf("module %q has no %s — was it hand-authored or created via `futi modules init`? Use --force to remove anyway", cfg.name, metayaml.FileName)
		}
		fmt.Fprintf(w, "Warning: no %s — skipping detach\n", metayaml.FileName)
	}

	if !cfg.yes {
		files, size := countTree(target)
		ok, err := confirmDelete(stdin, w, target, files, size)
		if err != nil {
			return fmt.Errorf("modules uninstall: %w", err)
		}
		if !ok {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	// detach перед удалением (§5.2 шаг 4). cwd = moduleRoot и для одиночного
	// yaml — это каталог с ним (корректно).
	if metaErr == nil && !cfg.skipDetach {
		// ParseModule без Validate: модуль с ошибкой схемы (опечатка ключа,
		// required+default) всё равно должен отработать detach; синтаксис
		// шаблона проверит рендер хука.
		mod, err := execmodule.ParseModule(manifestPath, moduleRoot)
		if err != nil {
			if cfg.strictDetach {
				return fmt.Errorf("cannot load manifest (strict), nothing deleted: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Warning: cannot load manifest, detach skipped: %v\n", err)
		} else if mod.Detach != "" {
			if err := modulerunner.RunHook(context.Background(), mod.Detach, moduleRoot, cfg.name, os.Stdout, os.Stderr); err != nil {
				if _, interrupted := interruptSignal(err); interrupted {
					return fmt.Errorf("detach interrupted, nothing deleted: %w", err)
				}
				if cfg.strictDetach {
					return fmt.Errorf("detach failed (strict), nothing deleted: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Warning: detach failed, proceeding with delete: %v\n", err)
			}
		}
	}

	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("modules uninstall: %w", err)
	}
	cleanupEmptyParents(filepath.Dir(target), roots)

	if err := cache.Invalidate(); err != nil {
		fmt.Fprintf(os.Stderr, "cache: %v (continuing)\n", err)
	}

	fmt.Fprintf(w, "Removed: %s\n", target)
	return nil
}

// cleanupEmptyParents удаляет пустые каталоги-родители (namespace-каталоги
// после удаления единственного модуля), пока не дойдём до одного из roots.
// Пути нормализуются (Abs, Clean, симлинки), поэтому хвостовой "/", "." и
// симлинки в корне не мешают его распознать; сам корень не удаляется никогда.
func cleanupEmptyParents(dir string, roots []string) {
	normRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		normRoots = append(normRoots, normalizePath(root))
	}
	dir = normalizePath(dir)
	for range 32 { // страховка от бесконечного цикла
		inside := false
		for _, root := range normRoots {
			if strictlyInside(root, dir) {
				inside = true
				break
			}
		}
		if !inside {
			return // dir == корень или вне корней
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
