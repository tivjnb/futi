package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/modulecache"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// writeMeta записывает lockfile при обновлении; переменная — шов для тестов.
var writeMeta = metayaml.Write

// removeBackup удаляет backup после успешного update; шов для тестов.
var removeBackup = os.RemoveAll

// updateArgs — аргументы `futi modules update`.
type updateArgs struct {
	name       string
	skipAttach bool
	yes        bool
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
		case arg == "--yes" || arg == "-y":
			cfg.yes = true
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
// stdin — для подтверждения замены файлов и attach-хуков (os.Stdin из main).
func runModulesUpdate(w io.Writer, stdin io.Reader, source discovery.Source, cache *modulecache.Cache, args []string) error {
	cfg, err := parseUpdateArgs(args)
	if err != nil {
		return err
	}

	gate := attachGate{in: bufio.NewReader(stdin), skip: cfg.skipAttach, yes: cfg.yes}
	if cfg.name != "" {
		return updateOne(w, gate, source, cache, cfg.name)
	}

	roots, err := readModuleRoots()
	if err != nil {
		return fmt.Errorf("modules update: %w", err)
	}
	scanned, backups := scanRoots(roots)
	// Модуль, видимый только через симлинк-каталог, не обновляем: замена
	// дерева превратила бы симлинк в копию, а оригинал остался бы старым.
	var entries []installedEntry
	for _, e := range scanned {
		if !e.ViaLink {
			entries = append(entries, e)
		}
	}
	for _, b := range backups {
		fmt.Fprintf(os.Stderr, "Warning: found %s left by an interrupted update; inspect it and remove or restore it manually\n", b)
	}
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
		if err := updateAt(w, gate, cache, e.Name, label, e.Dir, manifestPath); err != nil {
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
func updateOne(w io.Writer, gate attachGate, source discovery.Source, cache *modulecache.Cache, name string) error {
	manifestPath, moduleRoot, err := source.Resolve(name)
	if err != nil {
		return fmt.Errorf("resolve: %w%s", err, backupHint(name))
	}
	return updateAt(w, gate, cache, name, name, moduleRoot, manifestPath)
}

// backupHint возвращает подсказку про оставшийся backup модуля name в одном из
// корней или пустую строку. Путь строится в корне, чей префикс совпадает с
// началом имени (префикс снимается).
func backupHint(name string) string {
	roots, err := readModuleRoots()
	if err != nil {
		return ""
	}
	for _, root := range roots {
		relName, ok := stripRootPrefix(root.Prefix, name)
		if !ok {
			continue
		}
		rel := filepath.FromSlash(strings.ReplaceAll(relName, ".", "/"))
		backup := filepath.Join(root.Dir, rel) + backupSuffix
		if _, err := os.Lstat(backup); err == nil {
			return fmt.Sprintf(" (found %s left by an interrupted update; inspect it and remove or restore it manually)", backup)
		}
	}
	return ""
}

// updateAt обновляет модуль в конкретном каталоге moduleRoot. name — имя для
// хука attach, label — подпись в сообщениях (имя или имя с путём); gate
// решает, запускать ли хук.
func updateAt(w io.Writer, gate attachGate, cache *modulecache.Cache, name, label, moduleRoot, manifestPath string) error {
	if filepath.Base(manifestPath) != "config.yaml" {
		return fmt.Errorf("not a package module (updates require config.yaml packages)")
	}

	// Backup от прерванного update или неудавшегося отката может быть
	// единственной рабочей копией — не удаляем, пока пользователь не разберётся.
	backup := moduleRoot + backupSuffix
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("previous update left a backup at %s; inspect it and remove or restore it manually", backup)
	}

	metaPath := filepath.Join(moduleRoot, metayaml.FileName)
	meta, err := metayaml.Read(metaPath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("not installed via `futi modules install` (no %s): %w", metayaml.FileName, err)
	}
	if err != nil {
		return fmt.Errorf("invalid %s: %w", metayaml.FileName, err)
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

	// Триггер обновления — изменился источник, а не «диск отличается»: иначе
	// файлы, созданные attach-хуком, переустанавливали бы модуль каждый раз,
	// а локальные правки при неизменном источнике затирались бы.
	if newHash == lf.Hash {
		// No-op: обновляем только last_check (§5.3 шаг 6).
		meta.Lockfile.LastCheck = time.Now()
		if err := writeMeta(metaPath, meta); err != nil {
			return fmt.Errorf("write %s: %w", metayaml.FileName, err)
		}
		fmt.Fprintf(w, "%s: up to date\n", label)
		return nil
	}

	// Источник изменился — сравниваем с ним то, что лежит на диске.
	diff, err := metayaml.DiffTrees(moduleRoot, moduleDir)
	if err != nil {
		return err
	}
	if diff.Empty() {
		// Файлы те же (например, lockfile со старым форматом хеша): только
		// переписываем lockfile, дерево и attach не трогаем.
		meta.Lockfile.Hash = newHash
		meta.Lockfile.Commit = commit
		meta.Lockfile.LastCheck = time.Now()
		if err := writeMeta(metaPath, meta); err != nil {
			return fmt.Errorf("write %s: %w", metayaml.FileName, err)
		}
		fmt.Fprintf(w, "%s: up to date\n", label)
		return nil
	}
	// Подтверждение замены — до attach и до замены дерева: отказ оставляет
	// модуль как был, включая локальные правки.
	err = gate.allowReplace(label, moduleRoot, mod.Attach, diff)
	if errors.Is(err, errUpdateNotConfirmed) {
		return fmt.Errorf("%w; module not updated", err)
	}
	if err != nil {
		return err
	}

	// Подтверждение attach — до замены дерева: отказ оставляет модуль как был.
	runAttach, err := gate.allow(mod.Attach, name, moduleRoot)
	if errors.Is(err, errAttachNotConfirmed) {
		return fmt.Errorf("%w; module not updated", err)
	}
	if err != nil {
		return err
	}

	// Замена дерева: dest → backup, new → dest; при провале — восстановление.
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
	// Lockfile записан — backup больше не нужен. Ошибка удаления не фатальна, но
	// оставшийся backup заблокирует следующий update — предупреждаем.
	if err := removeBackup(backup); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: remove backup %s: %v\n", backup, err)
	}

	// attach: провал → warn, НЕ rollback (§3.1, §5.3 шаг 7).
	// Прерывание сигналом возвращаем после Invalidate: дерево уже заменено.
	var interruptErr error
	if runAttach {
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
