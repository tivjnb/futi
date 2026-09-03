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
