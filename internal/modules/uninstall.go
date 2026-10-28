package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// UninstallOptions are the inputs of Manager.Uninstall.
type UninstallOptions struct {
	Name         string
	Force        bool
	Yes          bool
	SkipDetach   bool
	StrictDetach bool
}

// Uninstall removes an installed module (spec §5.2). Confirmation goes through
// the manager's Prompter; the detach hook runs before the delete.
func (m *Manager) Uninstall(ctx context.Context, opts UninstallOptions) error {
	// Имя проверяем до Resolve: "" / "." иначе резолвятся в корень каталога модулей,
	// а "a/b" обходит проверки пакета в Resolve.
	if err := ValidateResolveName(opts.Name); err != nil {
		return err
	}
	source, err := m.rt.Source()
	if err != nil {
		return err
	}
	manifestPath, moduleRoot, err := source.Resolve(opts.Name)
	if err != nil {
		return fmt.Errorf("modules uninstall: %w", err)
	}

	readRoots, err := m.moduleRoots()
	if err != nil {
		return fmt.Errorf("modules uninstall: %w", err)
	}
	roots := rootDirs(readRoots)

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
		if !opts.Force {
			return fmt.Errorf("module %q has no %s — was it hand-authored or created via `futi modules init`? Use --force to remove anyway", opts.Name, metayaml.FileName)
		}
		fmt.Fprintf(m.s.Out, "Warning: no %s — skipping detach\n", metayaml.FileName)
	}

	if !opts.Yes {
		files, size := countTree(target)
		ok, err := confirmDelete(m.s.Prompt, m.s.Out, target, files, size)
		if err != nil {
			return fmt.Errorf("modules uninstall: %w", err)
		}
		if !ok {
			fmt.Fprintln(m.s.Out, "Aborted.")
			return nil
		}
	}

	// detach перед удалением (§5.2 шаг 4). cwd = moduleRoot и для одиночного
	// yaml — это каталог с ним (корректно).
	if metaErr == nil && !opts.SkipDetach {
		// ParseModule без Validate: модуль с ошибкой схемы (опечатка ключа,
		// required+default) всё равно должен отработать detach; синтаксис
		// шаблона проверит рендер хука.
		mod, err := execmodule.ParseModule(manifestPath, moduleRoot)
		if err != nil {
			if opts.StrictDetach {
				return fmt.Errorf("cannot load manifest (strict), nothing deleted: %w", err)
			}
			fmt.Fprintf(m.s.Err, "Warning: cannot load manifest, detach skipped: %v\n", err)
		} else if mod.Detach != "" {
			policy := m.rt.Exec()
			if err := modulerunner.RunHook(ctx, mod.Detach, moduleRoot, execmodule.EffectiveName(mod.Name, manifestPath), m.s.Stdin, m.s.Out, m.s.Err, policy.HookGroup, policy.ForwardSignals); err != nil {
				if _, interrupted := executor.InterruptSignal(err); interrupted {
					return fmt.Errorf("detach interrupted, nothing deleted: %w", err)
				}
				if opts.StrictDetach {
					return fmt.Errorf("detach failed (strict), nothing deleted: %w", err)
				}
				fmt.Fprintf(m.s.Err, "Warning: detach failed, proceeding with delete: %v\n", err)
			}
		}
	}

	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("modules uninstall: %w", err)
	}
	cleanupEmptyParents(filepath.Dir(target), roots)

	if err := m.rt.Cache().Invalidate(); err != nil {
		fmt.Fprintf(m.s.Err, "cache: %v (continuing)\n", err)
	}

	fmt.Fprintf(m.s.Out, "Removed: %s\n", target)
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
