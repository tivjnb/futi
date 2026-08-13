package execmodule

import (
	"strings"
	"testing"
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
	rendered, err := cmd.Render(map[string]string{"tag": "myapp", "file": "Dockerfile.dev"})
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
	rendered, err := cmd.Render(resolved)
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
	renderedFalse, err := cmd.Render(resolvedFalse)
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
	_, err := cmd.Render(map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing variable in template")
	}
}
