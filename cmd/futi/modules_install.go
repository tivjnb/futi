package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"futi/internal/config"
	"futi/internal/execmodule"
	"futi/internal/metayaml"
	"futi/internal/modulecache"
	"futi/internal/modulerunner"
)

// installArgs — аргументы `futi modules install`.
type installArgs struct {
	source      string
	local       bool
	namespace   string
	ref         string
	skipAttach  bool
	fromLocal   bool
	fromGit     bool
	fromArchive bool
}

// parseInstallArgs разбирает аргументы install (паттерн requireValue).
func parseInstallArgs(args []string) (*installArgs, error) {
	cfg := &installArgs{}
	var positional []string

	requireValue := func(flagName string, i int) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flagName)
		}
		val := args[i+1]
		if strings.HasPrefix(val, "--") {
			return "", fmt.Errorf("%s requires a value, got flag %s", flagName, val)
		}
		return val, nil
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--local":
			cfg.local = true
		case arg == "--skip-attach":
			cfg.skipAttach = true
		case arg == "--from-local":
			cfg.fromLocal = true
		case arg == "--from-git":
			cfg.fromGit = true
		case arg == "--from-archive":
			cfg.fromArchive = true
		case arg == "--namespace":
			val, err := requireValue("--namespace", i)
			if err != nil {
				return nil, err
			}
			cfg.namespace = val
			i++
		case strings.HasPrefix(arg, "--namespace="):
			cfg.namespace = strings.TrimPrefix(arg, "--namespace=")
		case arg == "--ref":
			val, err := requireValue("--ref", i)
			if err != nil {
				return nil, err
			}
			cfg.ref = val
			i++
		case strings.HasPrefix(arg, "--ref="):
			cfg.ref = strings.TrimPrefix(arg, "--ref=")
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
		i++
	}

	if len(positional) == 0 {
		return nil, fmt.Errorf("usage: futi modules install <source> [flags]")
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one source argument, got %d", len(positional))
	}
	cfg.source = positional[0]

	if cfg.namespace != "" {
		if err := validateModuleName(cfg.namespace); err != nil {
			return nil, fmt.Errorf("--namespace: %w", err)
		}
	}
	return cfg, nil
}

// installRoot возвращает корень установки: ./.futi при --local, иначе
// глобальный каталог (FUTI_MODULES_DIR или default).
func installRoot(local bool) (string, error) {
	if local {
		// Абсолютный путь: dest попадает в cwd и module_root хука.
		return filepath.Abs(".futi")
	}
	dir := os.Getenv("FUTI_MODULES_DIR")
	if dir == "" {
		var err error
		dir, err = config.DefaultModulesDir()
		if err != nil {
			return "", err
		}
	}
	return dir, nil
}

// runModulesInstall устанавливает модуль из источника (спека §5.1).
func runModulesInstall(w io.Writer, cache *modulecache.Cache, args []string) error {
	cfg, err := parseInstallArgs(args)
	if err != nil {
		return err
	}

	kind, err := detectSourceType(cfg.source, cfg.fromLocal, cfg.fromGit, cfg.fromArchive)
	if err != nil {
		return err
	}

	root, err := installRoot(cfg.local)
	if err != nil {
		return fmt.Errorf("modules install: %w", err)
	}

	// Fetch в temp-каталог; очистка при любом исходе.
	tempBase, err := os.MkdirTemp("", "futi-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempBase)

	moduleDir, commit, err := fetchSource(context.Background(), cfg.source, kind, cfg.ref, tempBase)
	if err != nil {
		return fmt.Errorf("modules install: %w", err)
	}

	// Валидация: config.yaml существует и проходит схему (§5.1 шаг 5).
	mod, err := execmodule.LoadModule(filepath.Join(moduleDir, "config.yaml"), moduleDir)
	if err != nil {
		return fmt.Errorf("source is not a futi module: %w", err)
	}

	name := mod.Name
	if name == "" {
		name = fallbackName(cfg.source)
	}
	if err := validateModuleName(name); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	if err := validateSegmentsNoDot(name); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}

	// Namespace в dot-форме ("images.lib") переводим в path-форму —
	// discovery резолвит "images.lib.qr" в images/lib/qr/.
	nsPath := filepath.FromSlash(strings.ReplaceAll(cfg.namespace, ".", "/"))
	dest := filepath.Join(root, nsPath, name)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("module already exists at %s; use `futi modules update`", dest)
	}
	// discovery видит и x.yaml, и x/config.yaml: оба сразу дали бы конфликт.
	if _, err := os.Stat(dest + ".yaml"); err == nil {
		return fmt.Errorf("module %q already exists at %s", name, dest+".yaml")
	}
	// Страховка от namespace-инъекций (validateModuleName уже отверг ..).
	rel, err := filepath.Rel(root, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("modules install: destination %s escapes modules directory %s", dest, root)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	if err := moveTree(moduleDir, dest); err != nil {
		return fmt.Errorf("modules install: move to destination: %w", err)
	}

	// __futi_meta.yaml: новый lockfile + about из источника (§5.1 шаг 7).
	hash, err := metayaml.ComputeTreeHash(dest)
	if err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	sourceMeta, _ := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	// Относительный local-источник привязываем к cwd установки: update
	// может запускаться из другого каталога.
	lockSource := cfg.source
	if kind == kindLocal {
		if lockSource, err = filepath.Abs(cfg.source); err != nil {
			return fmt.Errorf("modules install: %w", err)
		}
	}
	now := time.Now()
	meta := metayaml.MergeAbout(&metayaml.Meta{
		Lockfile: metayaml.Lockfile{
			Source:      lockSource,
			Type:        kind.String(),
			Ref:         cfg.ref,
			Commit:      commit,
			Hash:        hash,
			InstalledAt: now,
			LastCheck:   now,
		},
	}, sourceMeta)
	if err := metayaml.Write(filepath.Join(dest, metayaml.FileName), meta); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}

	// attach: провал → rollback (rm dest) + error (§3.1, §8 #6).
	if mod.Attach != "" && !cfg.skipAttach {
		if err := modulerunner.RunHook(context.Background(), mod.Attach, dest, name, os.Stdout, os.Stderr); err != nil {
			os.RemoveAll(dest)
			return fmt.Errorf("attach failed, module removed: %w", err)
		}
	}

	if err := cache.Invalidate(); err != nil {
		fmt.Fprintf(os.Stderr, "cache: %v (continuing)\n", err)
	}

	fmt.Fprintf(w, "Installed: %s → %s\n", cfg.source, dest)
	return nil
}
