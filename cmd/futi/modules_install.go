package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/modulerunner"
)

// InstallOptions are the inputs of Manager.Install.
type InstallOptions struct {
	Source      string
	Namespace   string
	Ref         string
	Local       bool
	SkipAttach  bool
	Yes         bool
	FromLocal   bool
	FromGit     bool
	FromArchive bool
}

// validateInstallOptions rejects a ref that could be taken for a git flag and
// a namespace that is not a valid module name or uses reserved segments.
func validateInstallOptions(opts InstallOptions) error {
	if strings.HasPrefix(opts.Ref, "-") {
		return fmt.Errorf("ref %q must not start with \"-\"", opts.Ref)
	}

	if opts.Namespace != "" {
		if err := validateModuleName(opts.Namespace); err != nil {
			return fmt.Errorf("--namespace: %w", err)
		}
		// validateModuleName допускает и "/", и "." как разделители — режем по обоим.
		if err := checkReservedSegments(strings.ReplaceAll(opts.Namespace, "/", "."), "."); err != nil {
			return fmt.Errorf("--namespace: %w", err)
		}
	}
	return nil
}

// installRootFrom returns the install root: <localDir>/.futi for a local
// install, otherwise globalDir.
func installRootFrom(local bool, localDir, globalDir string) (string, error) {
	if local {
		if localDir == "" {
			return "", errors.New("local modules are not available: no local directory")
		}
		// Абсолютный путь: dest попадает в cwd и module_root хука.
		return filepath.Abs(filepath.Join(localDir, ".futi"))
	}
	return globalDir, nil
}

// computeTreeHash считает хеш дерева модуля при install; шов для тестов.
var computeTreeHash = metayaml.ComputeTreeHash

// Install installs a module from a source (spec §5.1). Confirmation of the
// attach hook goes through the manager's Prompter.
func (m *Manager) Install(ctx context.Context, opts InstallOptions) (retErr error) {
	if err := validateInstallOptions(opts); err != nil {
		return err
	}

	kind, err := detectSourceType(opts.Source, opts.FromLocal, opts.FromGit, opts.FromArchive)
	if err != nil {
		return err
	}

	// F62: --ref осмыслен только для git; отказ до временных каталогов и сети.
	if opts.Ref != "" && kind != kindGit {
		return fmt.Errorf("modules install: --ref is only supported for git sources")
	}

	// Относительный local-источник привязываем к cwd установки: update
	// может запускаться из другого каталога. Считаем до переноса, чтобы ошибка
	// не оставила модуль без meta.
	lockSource := opts.Source
	if kind == kindLocal {
		if lockSource, err = filepath.Abs(opts.Source); err != nil {
			return fmt.Errorf("modules install: %w", err)
		}
	}

	root, err := installRootFrom(opts.Local, m.rt.LocalDir(), m.rt.ModulesDir())
	if err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	// Конфиг нужен только для сообщения о видимости глобальной установки,
	// но невалидный — ошибка до любых изменений на диске.
	var futiCfg *config.Config
	if !opts.Local {
		futiCfg = m.rt.Config()
	}

	// Fetch в temp-каталог; очистка при любом исходе.
	tempBase, err := os.MkdirTemp("", "futi-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempBase)

	moduleDir, commit, err := fetchSource(ctx, m.commandConfig(), opts.Source, kind, opts.Ref, tempBase)
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
		name = fallbackName(opts.Source)
	}
	if err := validateModuleName(name); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	if err := checkReservedSegments(name, "/"); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	if err := validateSegmentsNoDot(name); err != nil {
		if mod.Name == "" {
			return fmt.Errorf("modules install: %w; set name: in the source config.yaml", err)
		}
		return fmt.Errorf("modules install: %w", err)
	}

	// Namespace в dot-форме ("images.lib") переводим в path-форму —
	// discovery резолвит "images.lib.qr" в images/lib/qr/.
	nsPath := filepath.FromSlash(strings.ReplaceAll(opts.Namespace, ".", "/"))
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
	// Подтверждение attach — до любых изменений в каталоге модулей:
	// отказ ничего не оставляет на диске.
	gate := attachGate{p: m.s.Prompt, errW: m.s.Err, skip: opts.SkipAttach, yes: opts.Yes}
	runAttach, err := gate.allow(mod.Attach, name, dest)
	if errors.Is(err, errAttachNotConfirmed) {
		return fmt.Errorf("%w; module not installed", err)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	if err := moveTree(moduleDir, dest); err != nil {
		// Ошибка moveTree означает, что перенос не состоялся: частичный dest
		// он убрал сам, а существующий dst не трогал — откат здесь не нужен.
		return fmt.Errorf("modules install: move to destination: %w", err)
	}
	// Дерево перенесено: любой выход без committed (hash, meta, attach)
	// убирает dest, иначе модуль остался бы без meta и повторный install
	// упёрся бы в "already exists". Пустые каталоги namespace, созданные
	// MkdirAll, не убираем: они безвредны для discovery.
	committed := false
	defer func() {
		if committed {
			return
		}
		if rmErr := os.RemoveAll(dest); rmErr != nil {
			retErr = fmt.Errorf("%w (rollback failed: remove %s: %v)", retErr, dest, rmErr)
		}
	}()

	// __futi_meta.yaml: новый lockfile + about из источника (§5.1 шаг 7).
	hash, err := computeTreeHash(dest)
	if err != nil {
		return fmt.Errorf("modules install: %w", err)
	}
	sourceMeta, _ := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	now := time.Now()
	meta := metayaml.MergeAbout(&metayaml.Meta{
		Lockfile: metayaml.Lockfile{
			Source:      lockSource,
			Type:        kind.String(),
			Ref:         opts.Ref,
			Commit:      commit,
			Hash:        hash,
			InstalledAt: now,
			LastCheck:   now,
		},
	}, sourceMeta)
	if err := writeMeta(filepath.Join(dest, metayaml.FileName), meta); err != nil {
		return fmt.Errorf("modules install: %w", err)
	}

	// attach: провал → rollback (rm dest) + error (§3.1, §8 #6).
	if runAttach {
		if err := modulerunner.RunHook(ctx, mod.Attach, dest, execmodule.EffectiveName(mod.Name, filepath.Join(dest, "config.yaml")), m.s.Stdin, m.s.Out, m.s.Err, m.rt.Exec().HookGroup, m.rt.Exec().ForwardSignals); err != nil {
			return fmt.Errorf("attach failed, module removed: %w", err)
		}
	}

	committed = true

	if err := m.rt.Cache().Invalidate(); err != nil {
		fmt.Fprintf(m.s.Err, "cache: %v (continuing)\n", err)
	}

	fmt.Fprintf(m.s.Out, "Installed: %s → %s\n", opts.Source, dest)
	if !opts.Local {
		reportSourcesVisibility(m.s.Out, m.s.Err, futiCfg, root, dotName(filepath.Join(nsPath, name)))
	}
	return nil
}
