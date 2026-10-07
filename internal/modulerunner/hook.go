package modulerunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/executor"
)

// RunHook рендерит и выполняет hook-команду модуля (attach:/detach:,
// спека MODULE_MANAGMENT.md §3).
//
// Отличия от обычного cmd: результат рендера выполняется через `sh -c`
// (hook может быть составной командой: пайпы, &&, ;). stdin хука наследуется
// от futi (os.Stdin), таймаута нет. Доступные переменные
// шаблона: {{.module_name}}, {{.module_root}}, {{.env.FOO}}, {{quote .x}}.
// Env процесса: полное окружение пользователя + FUTI_MODULE_NAME,
// FUTI_MODULE_ROOT. cwd = moduleRoot. FUTI_CWD удалён (WORKDIR.md §2.4):
// cwd хука = moduleRoot, в скрипте getcwd() даёт то же.
// Без управляющего терминала хук запускается в своей группе процессов
// (executor.Config.IsolateWithoutTTY): сигналы futi получают и процессы,
// порождённые составной командой.
func RunHook(ctx context.Context, hookCmd, moduleRoot, moduleName string, stdout, stderr io.Writer) error {
	// Абсолютный корень: cwd (WithDir), FUTI_MODULE_ROOT и {{.module_root}}
	// должны указывать на один и тот же каталог.
	moduleRoot, err := filepath.Abs(moduleRoot)
	if err != nil {
		return fmt.Errorf("modulerunner: %w", err)
	}
	rendered, err := renderHook(hookCmd, moduleName, moduleRoot)
	if err != nil {
		return fmt.Errorf("modulerunner: %w", err)
	}

	env := os.Environ()
	env = append(env,
		"FUTI_MODULE_NAME="+moduleName,
		"FUTI_MODULE_ROOT="+moduleRoot,
	)

	cfg := executor.NewConfig().
		WithDir(moduleRoot).
		WithEnv(env).
		WithStdout(stdout).
		WithStderr(stderr)
	cfg.IsolateWithoutTTY = true
	return executor.RunWithConfig(ctx, cfg, "sh", "-c", rendered)
}

// renderHook рендерит hook-шаблон с тем же механизмом, что Command.Render
// (missingkey=error — опечатка в переменной падает, а не молчит).
func renderHook(hookCmd, moduleName, moduleRoot string) (string, error) {
	tmpl, err := template.New("hook").
		Funcs(execmodule.TemplateFuncs()).
		Option("missingkey=error").
		Parse(hookCmd)
	if err != nil {
		return "", &execmodule.TemplateError{Cmd: hookCmd, Cause: err}
	}
	data := map[string]interface{}{
		"module_name": moduleName,
		"module_root": moduleRoot,
		"env":         hookEnvMap(),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", &execmodule.TemplateError{Cmd: hookCmd, Cause: err}
	}
	return buf.String(), nil
}

// hookEnvMap собирает map окружения (как execmodule.envMapFn, локальная
// копия — не расширяем API execmodule).
func hookEnvMap() map[string]string {
	m := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.Index(kv, "="); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}
