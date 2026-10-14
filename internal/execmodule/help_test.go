package execmodule

import (
	"strings"
	"testing"
)

func TestFormatCommandHelpBasic(t *testing.T) {
	cmd := &Command{
		Description: "Build image",
		Params: []Param{
			{Name: "tag", Type: "string", Required: true},
			{Name: "file", Type: "string", Default: "Dockerfile"},
		},
		Cmd: "docker build -t {{.tag}}",
	}
	out := FormatCommandHelp("demo build", cmd)

	if !strings.Contains(out, "Build image") {
		t.Fatalf("expected description, got:\n%s", out)
	}
	if !strings.Contains(out, "futi demo build") {
		t.Fatalf("expected usage line, got:\n%s", out)
	}
	if !strings.Contains(out, "--tag") || !strings.Contains(out, "required") {
		t.Fatalf("expected --tag required param, got:\n%s", out)
	}
	if !strings.Contains(out, "--file") || !strings.Contains(out, "Dockerfile") {
		t.Fatalf("expected --file with default, got:\n%s", out)
	}
	if !strings.Contains(out, "docker build -t {{.tag}}") {
		t.Fatalf("expected cmd section, got:\n%s", out)
	}
}

func TestFormatCommandHelpPositional(t *testing.T) {
	cmd := &Command{
		Description: "Greet",
		Positional:  Positional{Min: 1},
		Cmd:         "echo {{index .args_list 0}}",
	}
	out := FormatCommandHelp("demo greet", cmd)
	if !strings.Contains(out, "at least 1 argument after") {
		t.Fatalf("expected positional section, got:\n%s", out)
	}
}

func TestFormatCommandHelpNoParamsNoPositional(t *testing.T) {
	cmd := &Command{
		Description: "List",
		Cmd:         "docker ps",
	}
	out := FormatCommandHelp("docker ps", cmd)
	if strings.Contains(out, "PARAMETERS") {
		t.Fatalf("expected no PARAMETERS section, got:\n%s", out)
	}
	if strings.Contains(out, "POSITIONAL") {
		t.Fatalf("expected no POSITIONAL section, got:\n%s", out)
	}
	if !strings.Contains(out, "docker ps") {
		t.Fatalf("expected cmd, got:\n%s", out)
	}
}

func TestFormatCommandHelpBoolDefault(t *testing.T) {
	cmd := &Command{
		Params: []Param{
			{Name: "excited", Type: "bool", Default: "false"},
		},
		Cmd: "echo hi",
	}
	out := FormatCommandHelp("demo greet", cmd)
	if !strings.Contains(out, "--excited") || !strings.Contains(out, "bool") || !strings.Contains(out, "false") {
		t.Fatalf("expected bool param with default, got:\n%s", out)
	}
}

func TestFormatModuleHelp(t *testing.T) {
	mod := &Module{
		Name:        "demo",
		Description: "Feature showcase",
		Cmd:         "echo default",
		Commands: map[string]*Command{
			"count": {Description: "Count up to N", Cmd: "seq 1 {{.n}}"},
			"greet": {Description: "Greet someone", Cmd: "echo hi"},
		},
	}
	out := FormatModuleHelp(mod, "demo")

	if !strings.Contains(out, "Feature showcase") {
		t.Fatalf("expected module description, got:\n%s", out)
	}
	if !strings.Contains(out, "echo default") {
		t.Fatalf("expected default cmd, got:\n%s", out)
	}
	if !strings.Contains(out, "count") || !strings.Contains(out, "greet") {
		t.Fatalf("expected commands listed, got:\n%s", out)
	}
	if !strings.Contains(out, "Count up to N") {
		t.Fatalf("expected command description, got:\n%s", out)
	}
}

func TestFormatModuleHelpNoDefaultNoDesc(t *testing.T) {
	mod := &Module{
		Name: "demo",
		Commands: map[string]*Command{
			"run": {Cmd: "echo hi"},
		},
	}
	out := FormatModuleHelp(mod, "demo")
	if strings.Contains(out, "echo default") {
		t.Fatalf("expected no default cmd, got:\n%s", out)
	}
	if !strings.Contains(out, "run") {
		t.Fatalf("expected command run, got:\n%s", out)
	}
}

func TestFormatModuleHelpDefaultParams(t *testing.T) {
	mod := &Module{
		Name:        "meta",
		Description: "Image stats",
		Cmd:         "echo {{.json}}",
		Params: []Param{
			{Name: "json", Type: "bool", Description: "JSON output"},
		},
		Positional: Positional{Min: 1},
	}
	out := FormatModuleHelp(mod, "images meta")

	if !strings.Contains(out, "default params:") {
		t.Fatalf("expected default params section, got:\n%s", out)
	}
	if !strings.Contains(out, "--json") || !strings.Contains(out, "JSON output") {
		t.Fatalf("expected --json with description, got:\n%s", out)
	}
	if !strings.Contains(out, "at least 1 argument") {
		t.Fatalf("expected positional hint, got:\n%s", out)
	}
}

// TestFormatModuleHelpStripsControlChars: ESC/OSC из YAML не доходят до терминала,
// а многострочный description сохраняет переводы строк (audit F18).
func TestFormatModuleHelpStripsControlChars(t *testing.T) {
	mod := &Module{
		Description: "line1\nline2\x1b]0;pwned\x07",
		Commands: map[string]*Command{
			"run": {Description: "d\x1b[2J", Cmd: "echo"},
		},
	}
	out := FormatModuleHelp(mod, "demo")
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x07") {
		t.Fatalf("output contains control chars: %q", out)
	}
	if !strings.Contains(out, "demo — line1\nline2]0;pwned\n") {
		t.Fatalf("multiline description not preserved: %q", out)
	}
}

func TestFormatCommandHelpStripsControlChars(t *testing.T) {
	cmd := &Command{
		Description: "a\nb\x1b[2J",
		Params:      []Param{{Name: "x", Default: "d\x1b[31m", Description: "pd\x1b[0m"}},
		Cmd:         "echo\x1b[1m",
	}
	out := FormatCommandHelp("demo run", cmd)
	if strings.Contains(out, "\x1b") {
		t.Fatalf("output contains ESC: %q", out)
	}
	if !strings.Contains(out, "demo run — a\nb[2J\n") {
		t.Fatalf("multiline description not preserved: %q", out)
	}
}

// TestFormatHelpAlignmentUsesSanitizedWidth: ширина колонки считается по
// очищенным именам (audit F18, review).
func TestFormatHelpAlignmentUsesSanitizedWidth(t *testing.T) {
	mod := &Module{Commands: map[string]*Command{
		"a\x1b[2Jbcdef": {Description: "d1"},
		"x":             {Description: "d2"},
	}}
	out := FormatModuleHelp(mod, "m")
	if !strings.Contains(out, "  x          d2\n") || !strings.Contains(out, "  a[2Jbcdef  d1\n") {
		t.Fatalf("misaligned: %q", out)
	}
}

// TestFormatHelpParamAlignmentUsesSanitizedWidth: ESC в имени параметра не
// сдвигает колонку описания ни в справке команды, ни в справке модуля.
func TestFormatHelpParamAlignmentUsesSanitizedWidth(t *testing.T) {
	params := []Param{
		{Name: "a\x1b[2Jbcdef", Type: "string", Description: "d1"},
		{Name: "x", Type: "string", Description: "d2"},
	}
	// Очищенное имя "a[2Jbcdef" — 9 символов, "x" дополняется 8 пробелами.
	wantLong := "  --a[2Jbcdef  (string)  d1\n"
	wantShort := "  --x          (string)  d2\n"

	out := FormatCommandHelp("m c", &Command{Params: params, Cmd: "echo"})
	if !strings.Contains(out, wantLong) || !strings.Contains(out, wantShort) {
		t.Errorf("FormatCommandHelp misaligned: %q", out)
	}
	out = FormatModuleHelp(&Module{Params: params}, "m")
	if !strings.Contains(out, wantLong) || !strings.Contains(out, wantShort) {
		t.Errorf("FormatModuleHelp misaligned: %q", out)
	}
}
