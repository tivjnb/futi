package execmodule

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/shlex"
)

func TestValidateRequiredParamMissing(t *testing.T) {
	cmd := &Command{
		Cmd: "docker build -t {{.tag}}",
		Params: []Param{
			{Name: "tag", Type: "string", Required: true},
		},
	}
	_, err := cmd.Validate(map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Fatalf("error should mention param name, got: %v", err)
	}
}

func TestValidateAppliesDefaults(t *testing.T) {
	cmd := &Command{
		Cmd: "docker build -t {{.tag}} -f {{.file}} .",
		Params: []Param{
			{Name: "tag", Type: "string", Required: true},
			{Name: "file", Type: "string", Default: "Dockerfile"},
		},
	}
	provided := map[string]string{"tag": "myapp"}
	resolved, err := cmd.Validate(provided)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["file"] != "Dockerfile" {
		t.Fatalf("expected default 'Dockerfile', got %q", resolved["file"])
	}
	if resolved["tag"] != "myapp" {
		t.Fatalf("expected 'myapp', got %q", resolved["tag"])
	}
}

func TestValidateUnknownParamReturnsError(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.msg}}",
		Params: []Param{
			{Name: "msg", Type: "string", Required: true},
		},
	}
	_, err := cmd.Validate(map[string]string{"msg": "hi", "unknown": "val"})
	if err == nil {
		t.Fatal("expected error for unknown param")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error should mention 'unknown', got: %v", err)
	}
}

func TestRenderSubstitutesVariables(t *testing.T) {
	cmd := &Command{
		Cmd: "docker build -t {{.tag}} -f {{.file}} .",
	}
	rendered, err := cmd.Render(map[string]string{"tag": "myapp", "file": "Dockerfile.dev"}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "docker build -t myapp -f Dockerfile.dev ."
	if rendered != expected {
		t.Fatalf("unexpected render: %q, want %q", rendered, expected)
	}
}

func TestRenderConditionalBlock(t *testing.T) {
	cmd := &Command{
		Cmd: "kubectl logs {{.pod}} {{if .follow}}-f{{end}}",
		Params: []Param{
			{Name: "pod", Type: "string", Required: true},
			{Name: "follow", Type: "bool"},
		},
	}

	// follow: "true" → {{if .follow}} рендерит блок
	resolved, err := cmd.Validate(map[string]string{"pod": "mypod", "follow": "true"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rendered, err := cmd.Render(resolved, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expected := "kubectl logs mypod -f"; rendered != expected {
		t.Fatalf("follow=true: unexpected render %q, want %q", rendered, expected)
	}

	// follow: "false" → {{if .follow}} НЕ рендерит блок
	resolvedFalse, err := cmd.Validate(map[string]string{"pod": "mypod", "follow": "false"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	renderedFalse, err := cmd.Render(resolvedFalse, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expectedFalse := "kubectl logs mypod "; renderedFalse != expectedFalse {
		t.Fatalf("follow=false: unexpected render %q, want %q", renderedFalse, expectedFalse)
	}
}

func TestValidateNormalizesBoolParams(t *testing.T) {
	cases := []struct {
		name     string
		defaul   string
		provided map[string]string
		want     string
	}{
		{"default false", "false", nil, ""},
		{"default true", "true", nil, "true"},
		{"provided false", "", map[string]string{"follow": "false"}, ""},
		{"provided 0", "", map[string]string{"follow": "0"}, ""},
		{"provided no", "", map[string]string{"follow": "no"}, ""},
		{"provided empty", "", map[string]string{"follow": ""}, ""},
		{"provided true", "", map[string]string{"follow": "true"}, "true"},
		{"provided 1", "", map[string]string{"follow": "1"}, "true"},
		{"provided yes", "", map[string]string{"follow": "yes"}, "true"},
		{"provided other is truthy", "", map[string]string{"follow": "anything"}, "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &Command{
				Params: []Param{{Name: "follow", Type: "bool", Default: tc.defaul}},
			}
			resolved, err := cmd.Validate(tc.provided)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resolved["follow"] != tc.want {
				t.Fatalf("got %q, want %q", resolved["follow"], tc.want)
			}
		})
	}
}

func TestRenderMissingVariableReturnsError(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.msg}}",
	}
	_, err := cmd.Render(map[string]string{}, "", nil)
	if err == nil {
		t.Fatal("expected error for missing variable in template")
	}
}

func TestRenderIndexOutOfRangeReturnsTemplateError(t *testing.T) {
	// Доступ к несуществующему индексу args_list → TemplateError с шаблоном и причиной.
	cmd := &Command{
		Cmd: "echo {{index .args_list 0}}",
	}
	_, err := cmd.Render(map[string]string{}, "", nil) // args_list пуст
	if err == nil {
		t.Fatal("expected error for index out of range")
	}
	var tmplErr *TemplateError
	if !errors.As(err, &tmplErr) {
		t.Fatalf("expected *TemplateError, got %T", err)
	}
	if tmplErr.Cmd != "echo {{index .args_list 0}}" {
		t.Fatalf("unexpected Cmd: %q", tmplErr.Cmd)
	}
	if tmplErr.Cause == nil {
		t.Fatal("expected non-nil Cause")
	}
	if !strings.Contains(tmplErr.Cause.Error(), "out of range") {
		t.Fatalf("expected 'out of range' in cause, got %q", tmplErr.Cause.Error())
	}
}

func TestRenderParseErrorReturnsTemplateError(t *testing.T) {
	// Невалидный синтаксис шаблона → TemplateError.
	cmd := &Command{
		Cmd: "echo {{.name",
	}
	_, err := cmd.Render(map[string]string{}, "", nil)
	if err == nil {
		t.Fatal("expected parse error")
	}
	var tmplErr *TemplateError
	if !errors.As(err, &tmplErr) {
		t.Fatalf("expected *TemplateError, got %T", err)
	}
	if tmplErr.Cmd != "echo {{.name" {
		t.Fatalf("unexpected Cmd: %q", tmplErr.Cmd)
	}
}

func TestRenderSubstitutesArgs(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.args}}",
	}
	rendered, err := cmd.Render(map[string]string{}, "hello world", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "echo hello world" {
		t.Fatalf("unexpected render: %q, want %q", rendered, "echo hello world")
	}
}

func TestRenderSubstitutesArgsList(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{index .args_list 0}} {{index .args_list 1}}",
	}
	rendered, err := cmd.Render(map[string]string{}, "", []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "echo hello world" {
		t.Fatalf("unexpected render: %q, want %q", rendered, "echo hello world")
	}
}

func TestRenderArgsAndParams(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.name}} {{.args}}",
	}
	rendered, err := cmd.Render(map[string]string{"name": "alice"}, "hello world", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "echo alice hello world" {
		t.Fatalf("unexpected render: %q, want %q", rendered, "echo alice hello world")
	}
}

func TestRenderArgsEmpty(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.args}}",
	}
	rendered, err := cmd.Render(map[string]string{}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "echo " {
		t.Fatalf("unexpected render: %q, want %q", rendered, "echo ")
	}
}

func TestRenderArgsListEmpty(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.args}}",
	}
	rendered, err := cmd.Render(map[string]string{}, "", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "echo " {
		t.Fatalf("unexpected render: %q, want %q", rendered, "echo ")
	}
}

func TestValidateIntParamValid(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.count}}",
		Params: []Param{
			{Name: "count", Type: "int", Required: true},
		},
	}
	resolved, err := cmd.Validate(map[string]string{"count": "42"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["count"] != "42" {
		t.Fatalf("expected %q, got %q", "42", resolved["count"])
	}
}

func TestValidateIntParamInvalid(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.count}}",
		Params: []Param{
			{Name: "count", Type: "int", Required: true},
		},
	}
	_, err := cmd.Validate(map[string]string{"count": "abc"})
	if err == nil {
		t.Fatal("expected error for non-int value")
	}
	if !strings.Contains(err.Error(), "count") || !strings.Contains(err.Error(), "int") {
		t.Fatalf("error should mention param name and type, got: %v", err)
	}
}

func TestValidateIntParamDefault(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.count}}",
		Params: []Param{
			{Name: "count", Type: "int", Default: "10"},
		},
	}
	resolved, err := cmd.Validate(map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["count"] != "10" {
		t.Fatalf("expected default %q, got %q", "10", resolved["count"])
	}
}

func TestValidateIntParamNotRequiredEmpty(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.count}}",
		Params: []Param{
			{Name: "count", Type: "int"},
		},
	}
	resolved, err := cmd.Validate(map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["count"] != "" {
		t.Fatalf("expected empty, got %q", resolved["count"])
	}
}

func TestValidateIntParamNegative(t *testing.T) {
	cmd := &Command{
		Cmd: "echo {{.count}}",
		Params: []Param{
			{Name: "count", Type: "int", Required: true},
		},
	}
	resolved, err := cmd.Validate(map[string]string{"count": "-5"})
	if err != nil {
		t.Fatalf("unexpected error for negative int: %v", err)
	}
	if resolved["count"] != "-5" {
		t.Fatalf("expected %q, got %q", "-5", resolved["count"])
	}
}

func TestRenderEnvVar(t *testing.T) {
	t.Setenv("FUTI_TEST_VAR", "hello")
	cmd := &Command{Cmd: "echo {{.env.FUTI_TEST_VAR}}"}
	rendered, err := cmd.Render(map[string]string{}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "echo hello" {
		t.Fatalf("expected 'echo hello', got %q", rendered)
	}
}

func TestRenderEnvVarMissing(t *testing.T) {
	cmd := &Command{Cmd: "echo {{.env.FUTI_DEFINITELY_NONEXISTENT_VAR_XYZ}}"}
	_, err := cmd.Render(map[string]string{}, "", nil)
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

func TestRenderEnvVarConditional(t *testing.T) {
	t.Setenv("FUTI_TEST_COND", "yes")
	cmd := &Command{Cmd: "{{if index .env \"FUTI_TEST_COND\"}}set{{else}}unset{{end}}"}
	rendered, err := cmd.Render(map[string]string{}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "set" {
		t.Fatalf("expected 'set', got %q", rendered)
	}
}

func TestRenderEnvVarConditionalMissing(t *testing.T) {
	cmd := &Command{Cmd: "{{if index .env \"FUTI_NOPE_NOTHERE_XYZ\"}}set{{else}}unset{{end}}"}
	rendered, err := cmd.Render(map[string]string{}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "unset" {
		t.Fatalf("expected 'unset', got %q", rendered)
	}
}

func TestRenderEnvVarWithParams(t *testing.T) {
	t.Setenv("FUTI_TEST_TOKEN", "secret123")
	cmd := &Command{
		Cmd: "deploy --token {{.env.FUTI_TEST_TOKEN}} --env {{.env_name}}",
		Params: []Param{
			{Name: "env_name", Type: "string", Required: true},
		},
	}
	rendered, err := cmd.Render(map[string]string{"env_name": "staging"}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "deploy --token secret123 --env staging"
	if rendered != expected {
		t.Fatalf("expected %q, got %q", expected, rendered)
	}
}

// TestEnvMapFnOverridable проверяет, что envMapFn — package-level var,
// которую можно заменить (фикс #43 — кэширование для HTTP server).
func TestEnvMapFnOverridable(t *testing.T) {
	original := envMapFn
	defer func() { envMapFn = original }()

	envMapFn = func() map[string]string {
		return map[string]string{"CUSTOM": "value"}
	}

	cmd := &Command{Cmd: "echo {{.env.CUSTOM}}"}
	rendered, err := cmd.Render(map[string]string{}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered != "echo value" {
		t.Fatalf("expected 'echo value', got %q", rendered)
	}
}

func TestValidatePositionalOK(t *testing.T) {
	cmd := &Command{Positional: Positional{Min: 2}}
	if err := cmd.ValidatePositional([]string{"a", "b"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cmd.ValidatePositional([]string{"a", "b", "c"}); err != nil {
		t.Fatalf("more args than min should pass: %v", err)
	}
}

func TestValidatePositionalTooFew(t *testing.T) {
	cmd := &Command{Positional: Positional{Min: 2}}
	err := cmd.ValidatePositional([]string{"a"})
	if err == nil {
		t.Fatal("expected error for too few positional args")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("expected min in error, got: %v", err)
	}
}

func TestValidatePositionalMinZero(t *testing.T) {
	cmd := &Command{Positional: Positional{Min: 0}}
	if err := cmd.ValidatePositional(nil); err != nil {
		t.Fatalf("min 0 should always pass: %v", err)
	}
}
// --- {{quote}} (REV-004: защита от argv/flag-инъекции через re-shlex) ---

// TestQuoteShlexRoundTrip проверяет, что quoteShlex(v) после shlex.Split
// возвращает ровно [v]: значение с любыми кавычками/метасимволами остаётся
// одним argv-токеном. Это контракт совместимости POSIX-экранирования с
// используемым shlex-парсером.
func TestQuoteShlexRoundTrip(t *testing.T) {
	values := []string{
		"hello",
		"hello world",
		`x" --force "y`,
		"it's quoted",
		`$HOME`,
		"; rm -rf /tmp/pwn",
		"",
		`back\slash`,
		"'quoted'",
		"'" + `mix'"ed` + "'",
		"a\tb",
	}
	for _, v := range values {
		quoted := quoteShlex(v)
		parts, err := shlex.Split(quoted)
		if err != nil {
			t.Fatalf("quoteShlex(%q) = %q: shlex.Split error: %v", v, quoted, err)
		}
		if len(parts) != 1 || parts[0] != v {
			t.Fatalf("quoteShlex(%q) = %q: shlex.Split = %v, want [%q]", v, quoted, parts, v)
		}
	}
}

// TestRenderQuotePreventsFlagInjection проверяет сценарий REV-004:
// пользователь передаёт значение с кавычками и флагом, шаблон использует
// {{quote .msg}} — значение НЕ пробивает границы argv, флаг --force
// остаётся частью значения, а не отдельным argv.
func TestRenderQuotePreventsFlagInjection(t *testing.T) {
	cmd := &Command{Cmd: `echo {{quote .msg}}`}
	rendered, err := cmd.Render(map[string]string{"msg": `x" --force "y`}, "", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	parts, err := shlex.Split(rendered)
	if err != nil {
		t.Fatalf("shlex.Split(%q): %v", rendered, err)
	}
	want := []string{"echo", `x" --force "y`}
	if len(parts) != len(want) {
		t.Fatalf("argv = %v, want %v (flag injection!)", parts, want)
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Fatalf("argv = %v, want %v (flag injection!)", parts, want)
		}
	}

	// Явная проверка: --force не стал отдельным argv-токеном.
	for _, p := range parts[1:] {
		if p == "--force" {
			t.Fatalf("flag injection: --force became a separate argv token in %v", parts)
		}
	}
}

// TestRenderQuoteArgsList проверяет использование quote с элементом args_list:
// {{quote (index .args_list 0)}} — аргумент с пробелами/кавычками после "--"
// остаётся одним argv-токеном.
func TestRenderQuoteArgsList(t *testing.T) {
	cmd := &Command{Cmd: `echo {{quote (index .args_list 0)}}`}
	rendered, err := cmd.Render(map[string]string{}, "", []string{`a b "c --force`})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	parts, err := shlex.Split(rendered)
	if err != nil {
		t.Fatalf("shlex.Split(%q): %v", rendered, err)
	}
	want := []string{"echo", `a b "c --force`}
	if len(parts) != 2 || parts[0] != "echo" || parts[1] != want[1] {
		t.Fatalf("argv = %v, want %v", parts, want)
	}
}

// TestRenderQuotePlainValue проверяет, что quote не портит обычные значения.
func TestRenderQuotePlainValue(t *testing.T) {
	cmd := &Command{Cmd: `echo {{quote .msg}}`}
	rendered, err := cmd.Render(map[string]string{"msg": "hello"}, "", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered != `echo 'hello'` {
		t.Fatalf("rendered = %q, want %q", rendered, `echo 'hello'`)
	}
}
