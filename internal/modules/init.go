package modules

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/execmodule"
)

// InitOptions are the parameters of `futi modules init`.
type InitOptions struct {
	Path        string   // путь модуля (может содержать / для namespace)
	Global      bool     // создавать в ~/.config/futi/modules/ вместо CWD
	Description string   // описание модуля
	DefaultCmd  string   // cmd на уровне модуля
	Commands    []string // имена команд для шаблона
}

// ValidateInitPath checks the module path given to init: the name must be
// valid and no segment may be a reserved word.
func ValidateInitPath(path string) error {
	// F57: зарезервированное слово запрещено в любом сегменте пути.
	trimmed := path
	if strings.HasSuffix(trimmed, ".yaml") {
		trimmed = strings.TrimSuffix(trimmed, ".yaml")
	} else {
		trimmed = strings.TrimSuffix(trimmed, ".yml")
	}
	if err := validateModuleName(filepath.ToSlash(trimmed)); err != nil {
		return err
	}
	return checkReservedSegments(filepath.ToSlash(trimmed), "/")
}

// targetFilePath возвращает путь к файлу модуля.
// Локально: ./.futi/<path>.yaml (доступен через auto-discovery).
// Глобально: <globalDir>/<path>.yaml (globalDir — из core.DefaultModulesDir).
// #40: отклоняет path с .. (path traversal за пределы base).
// #45: не добавляет .yaml, если path уже заканчивается на .yaml/.yml.
func targetFilePath(opts *InitOptions, globalDir string) (string, error) {
	base := ".futi"
	if opts.Global {
		base = globalDir
	}

	// #45: не добавляем .yaml, если уже есть расширение.
	path := opts.Path
	if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
		path += ".yaml"
	}

	fullPath := filepath.Join(base, path)

	// #40: проверяем, что результат внутри base (нет path traversal).
	rel, err := filepath.Rel(base, fullPath)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the modules directory", opts.Path)
	}
	if err := validateSegmentsNoDot(strings.TrimSuffix(strings.TrimSuffix(rel, ".yaml"), ".yml")); err != nil {
		return "", err
	}
	// discovery видит и x.yaml, и x/config.yaml: оба сразу дали бы конфликт.
	if strings.HasSuffix(fullPath, ".yaml") {
		dirConfig := filepath.Join(strings.TrimSuffix(fullPath, ".yaml"), "config.yaml")
		if _, err := os.Stat(dirConfig); err == nil {
			return "", fmt.Errorf("module %q already exists at %s", opts.Path, dirConfig)
		}
	}
	// Симметрично: x/config.yaml при существующем x.yaml.
	if filepath.Base(fullPath) == "config.yaml" && filepath.Dir(fullPath) != base {
		siblingYAML := filepath.Dir(fullPath) + ".yaml"
		if _, err := os.Stat(siblingYAML); err == nil {
			return "", fmt.Errorf("module %q already exists at %s", opts.Path, siblingYAML)
		}
	}

	return fullPath, nil
}

// initModuleName — dot-имя, под которым discovery найдёт модуль, созданный
// по пути path: без расширения, у пакета (<dir>/config.yaml) — имя каталога.
func initModuleName(path string) string {
	rel := strings.TrimSuffix(strings.TrimSuffix(filepath.ToSlash(path), ".yaml"), ".yml")
	if dir, base := filepath.Split(rel); base == "config" && dir != "" {
		rel = strings.TrimSuffix(dir, "/")
	}
	return dotName(rel)
}

// generateModuleYAML генерирует YAML-шаблон модуля.
// name — имя модуля (из последнего сегмента path).
// description — описание (квотируется для безопасности — фикс #41).
// defaultCmd — cmd на уровне модуля (пусто = не добавлять).
// commands — имена команд (если пусто — одна команда hello по умолчанию).
// #46: name квотируется. #47: ключи команд квотируются. F62: имя команды в
// cmd квотируется для shlex — argv всегда [echo, <имя>].
func generateModuleYAML(name, description, defaultCmd string, commands []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", quoteYAMLValue(name))
	fmt.Fprintf(&b, "description: %s\n", quoteYAMLValue(description))
	if defaultCmd != "" {
		fmt.Fprintf(&b, "cmd: %s\n", quoteYAMLValue(defaultCmd))
	}
	b.WriteString("commands:\n")
	if len(commands) > 0 {
		for _, cmd := range commands {
			fmt.Fprintf(&b, "  %s:\n    cmd: %s\n", quoteYAMLValue(cmd), quoteYAMLValue("echo "+execmodule.QuoteShlex(cmd)))
		}
	} else {
		b.WriteString("  \"hello\":\n    cmd: \"echo hello\"\n")
	}
	return b.String()
}

// quoteYAMLValue оборачивает значение в двойные кавычки для YAML.
// Использует Go-экранирование (%q), которое совместимо с YAML double-quoted style.
func quoteYAMLValue(s string) string {
	return fmt.Sprintf("%q", s)
}

// Init создаёт новый модуль с шаблоном YAML (подкоманда `futi modules init`).
// Генерирует шаблон и пишет файл; сообщения идут в out, предупреждения — в errOut.
// Возвращает ошибку при существующем файле (без перезаписи).
// #44: использует O_EXCL вместо Stat+WriteFile для атомарной проверки.
func Init(out, errOut io.Writer, opts InitOptions) error {
	if err := ValidateInitPath(opts.Path); err != nil {
		return err
	}
	if opts.Description == "" {
		opts.Description = "My tool"
	}

	// Глобальный каталог и config.yaml — до записи: ошибка не должна
	// прийти после «Created:». Конфиг нужен для сообщения о видимости модуля.
	var err error
	var globalDir string
	var futiCfg *config.Config
	if opts.Global {
		if globalDir, err = core.DefaultModulesDir(); err != nil {
			return fmt.Errorf("modules dir: %w", err)
		}
		if futiCfg, err = core.LoadDefaultConfig(); err != nil {
			return err
		}
	}
	targetPath, err := targetFilePath(&opts, globalDir)
	if err != nil {
		return err
	}

	name := filepath.Base(opts.Path)
	name = strings.TrimSuffix(name, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	yaml := generateModuleYAML(name, opts.Description, opts.DefaultCmd, opts.Commands)

	// Сгенерированный манифест проверяется правилами схемы до записи:
	// иначе init сообщит об успехе, а модуль не загрузится.
	mod, err := execmodule.ParseManifest([]byte(yaml), targetPath, filepath.Dir(targetPath))
	if err != nil {
		return fmt.Errorf("invalid module: %w", err)
	}
	if errs, _ := mod.Validate(); len(errs) > 0 {
		return fmt.Errorf("invalid module: %w", errs[0])
	}

	dir := filepath.Dir(targetPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	// #44: O_EXCL — атомарная проверка существования без TOCTOU race.
	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists", targetPath)
		}
		return fmt.Errorf("create file: %w", err)
	}
	if _, err := f.Write([]byte(yaml)); err != nil {
		f.Close()
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	// Показываем реальный путь записи: EvalSymlinks резолвит симлинки.
	// При записи через симлинк-директорию (dotfiles-репо, dev-симлинк)
	// файл физически попадает в целевой каталог симлинка — сообщаем его.
	displayPath := targetPath
	if resolved, err := filepath.EvalSymlinks(targetPath); err == nil {
		displayPath = resolved
	}
	fmt.Fprintf(out, "Created: %s\n", displayPath)
	if opts.Global {
		reportSourcesVisibility(out, errOut, futiCfg, globalDir, initModuleName(opts.Path))
	}
	return nil
}
