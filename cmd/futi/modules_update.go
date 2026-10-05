package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"futi/internal/discovery"
	"futi/internal/execmodule"
	"futi/internal/metayaml"
	"futi/internal/modulecache"
	"futi/internal/modulerunner"
)

// writeMeta записывает lockfile при обновлении; переменная — шов для тестов.
var writeMeta = metayaml.Write

// updateArgs — аргументы `futi modules update`.
type updateArgs struct {
	name       string
	skipAttach bool
}

// parseUpdateArgs разбирает аргументы update.
func parseUpdateArgs(args []string) (*updateArgs, error) {
	cfg := &updateArgs{}
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--skip-attach":
			cfg.skipAttach = true
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) > 1 {
		return nil, fmt.Errorf("expected at most one module name, got %d", len(positional))
	}
	if len(positional) == 1 {
		cfg.name = positional[0]
		if err := validateResolveName(cfg.name); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// runModulesUpdate переустанавливает модуль(ы) из источника (спека §5.3).
// Без имени — обновляет все установленные модули (serial, MVP).
func runModulesUpdate(w io.Writer, source discovery.Source, cache *modulecache.Cache, args []string) error {
	cfg, err := parseUpdateArgs(args)
	if err != nil {
		return err
	}

	if cfg.name != "" {
		return updateOne(w, source, cache, cfg.name, cfg.skipAttach)
	}

	roots, err := moduleRoots()
	if err != nil {
		return fmt.Errorf("modules update: %w", err)
	}
	entries := scanInstalled(roots)
	if len(entries) == 0 {
		fmt.Fprintln(w, "No installed modules found.")
		return nil
	}

	// Одноимённые модули из разных корней различаем в выводе путём каталога.
	counts := make(map[string]int)
	for _, e := range entries {
		counts[e.Name]++
	}

	var failed []string
	for _, e := range entries {
		label := e.Name
		if counts[e.Name] > 1 {
			label = fmt.Sprintf("%s (%s)", e.Name, e.Dir)
		}
		// Обновляем именно найденный каталог, а не то, что отдаст Resolve по имени.
		manifestPath := filepath.Join(e.Dir, "config.yaml")
		if err := updateAt(w, cache, e.Name, label, e.Dir, manifestPath, cfg.skipAttach); err != nil {
			// Прерывание печатает main возвращённой ошибкой — здесь не дублируем.
			if _, interrupted := interruptSignal(err); interrupted {
				return fmt.Errorf("update %s interrupted: %w", label, err)
			}
			fmt.Fprintf(os.Stderr, "update %s: %v\n", label, err)
			failed = append(failed, label)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to update: %s", strings.Join(failed, ", "))
	}
	return nil
}

// updateOne обновляет один модуль (спека §5.3).
func updateOne(w io.Writer, source discovery.Source, cache *modulecache.Cache, name string, skipAttach bool) error {
	manifestPath, moduleRoot, err := source.Resolve(name)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	return updateAt(w, cache, name, name, moduleRoot, manifestPath, skipAttach)
}

// updateAt обновляет модуль в конкретном каталоге moduleRoot. name — имя для
// хука attach, label — подпись в сообщениях (имя или имя с путём).
func updateAt(w io.Writer, cache *modulecache.Cache, name, label, moduleRoot, manifestPath string, skipAttach bool) error {
	if filepath.Base(manifestPath) != "config.yaml" {
		return fmt.Errorf("not a package module (updates require config.yaml packages)")
	}

	metaPath := filepath.Join(moduleRoot, metayaml.FileName)
	meta, err := metayaml.Read(metaPath)
	if err != nil {
		return fmt.Errorf("not installed via `futi modules install` (no %s): %w", metayaml.FileName, err)
	}

	lf := meta.Lockfile
	var kind sourceKind
	switch lf.Type {
	case "local":
		kind = kindLocal
	case "git":
		kind = kindGit
	case "archive":
		kind = kindArchive
	default:
		return fmt.Errorf("unknown source type %q in %s", lf.Type, metayaml.FileName)
	}

	tempBase, err := os.MkdirTemp("", "futi-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempBase)

	moduleDir, commit, err := fetchSource(context.Background(), lf.Source, kind, lf.Ref, tempBase)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	mod, err := execmodule.LoadModule(filepath.Join(moduleDir, "config.yaml"), moduleDir)
	if err != nil {
		return fmt.Errorf("source is not a futi module: %w", err)
	}

	newHash, err := metayaml.ComputeTreeHash(moduleDir)
	if err != nil {
		return err
	}

	if newHash == lf.Hash {
		// No-op: обновляем только last_check (§5.3 шаг 6).
		meta.Lockfile.LastCheck = time.Now()
		if err := metayaml.Write(metaPath, meta); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s: up to date\n", label)
		return nil
	}

	// Замена дерева: dest → backup, new → dest; при провале — восстановление.
	backup := moduleRoot + ".futi-backup"
	// Остаток backup от прошлого сбоя (moduleRoot существует) не должен мешать.
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove stale backup %s: %w", backup, err)
	}
	if err := os.Rename(moduleRoot, backup); err != nil {
		return fmt.Errorf("backup old tree: %w", err)
	}
	if err := moveTree(moduleDir, moduleRoot); err != nil {
		return restoreBackup(backup, moduleRoot, fmt.Errorf("install new tree: %w", err))
	}

	// Lockfile: новый hash/commit/last_check; InstalledAt сохраняем.
	// About: из источника (если есть), иначе старый (§4.1).
	sourceMeta, _ := metayaml.Read(filepath.Join(moduleRoot, metayaml.FileName))
	newMeta := metayaml.MergeAbout(&metayaml.Meta{
		Lockfile: metayaml.Lockfile{
			Source:      lf.Source,
			Type:        lf.Type,
			Ref:         lf.Ref,
			Commit:      commit,
			Hash:        newHash,
			InstalledAt: lf.InstalledAt,
			LastCheck:   time.Now(),
		},
	}, sourceMeta)
	if newMeta.About == nil {
		newMeta.About = meta.About // старый about, если в источнике нет
	}
	if err := writeMeta(metaPath, newMeta); err != nil {
		return restoreBackup(backup, moduleRoot, fmt.Errorf("write %s: %w", metayaml.FileName, err))
	}
	// Lockfile записан — backup больше не нужен (при ошибке удаления его уберёт
	// следующий update).
	os.RemoveAll(backup)

	// attach: провал → warn, НЕ rollback (§3.1, §5.3 шаг 7).
	// Прерывание сигналом возвращаем после Invalidate: дерево уже заменено.
	var interruptErr error
	if mod.Attach != "" && !skipAttach {
		if err := modulerunner.RunHook(context.Background(), mod.Attach, moduleRoot, name, os.Stdout, os.Stderr); err != nil {
			if _, interrupted := interruptSignal(err); interrupted {
				interruptErr = fmt.Errorf("attach interrupted: %w", err)
			} else {
				fmt.Fprintf(os.Stderr, "Warning: attach failed; module may be partially initialized: %v\n", err)
			}
		}
	}

	if err := cache.Invalidate(); err != nil {
		fmt.Fprintf(os.Stderr, "cache: %v (continuing)\n", err)
	}
	if interruptErr != nil {
		return interruptErr
	}

	fmt.Fprintf(w, "%s: updated\n", label)
	return nil
}

// restoreBackup возвращает старое дерево из backup на место moduleRoot,
// предварительно удалив новое (возможно, частичное). cause — исходная ошибка;
// сбой отката дописывается к ней вместе с путём backup, где лежит рабочая копия.
func restoreBackup(backup, moduleRoot string, cause error) error {
	if err := os.RemoveAll(moduleRoot); err != nil {
		return fmt.Errorf("%w (rollback failed: remove %s: %v; old tree is in %s)", cause, moduleRoot, err, backup)
	}
	if err := os.Rename(backup, moduleRoot); err != nil {
		return fmt.Errorf("%w (rollback failed: %v; old tree is in %s)", cause, err, backup)
	}
	return cause
}
