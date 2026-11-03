package core_test

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

// exposeDir writes: module ops (root cli+mcp; status inherits, prod mcp
// only), ns/hidden (mcp only) and ns/shown.
func exposeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ops.yaml"), `expose: [cli, mcp]
commands:
  status:
    cmd: echo status
  prod:
    expose: [mcp]
    cmd: echo prod
  grp:
    expose: [mcp]
    commands:
      a:
        cmd: echo a
`)
	writeFile(t, filepath.Join(dir, "ng", "nocmd.yaml"), "expose: [mcp]\ncommands:\n  a:\n    cmd: echo a\n")
	writeFile(t, filepath.Join(dir, "ns", "hidden.yaml"), "expose: [mcp]\ncmd: echo hidden\n")
	writeFile(t, filepath.Join(dir, "ns", "shown.yaml"), "cmd: echo shown\n")
	return dir
}

func exposeModules(t *testing.T) discovery.Source {
	t.Helper()
	return discovery.NewGlobalSource(exposeDir(t))
}

func exposeRT(t *testing.T) *core.Runtime {
	t.Helper()
	rt, err := core.NewRuntime(core.Options{ModulesDir: exposeDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestResolveWordsNotExposed(t *testing.T) {
	rt := exposeRT(t)

	_, _, err := rt.ResolveWords(execmodule.FrontendCLI, []string{"ops", "prod"})
	var ne *core.NotExposedError
	if !errors.As(err, &ne) {
		t.Fatalf("err = %v, want *NotExposedError", err)
	}
	if !slices.Equal(ne.Path, []string{"ops", "prod"}) {
		t.Errorf("Path = %q, want [ops prod]", ne.Path)
	}
	want := `command "futi ops prod" is not available in the CLI (expose: mcp)`
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	if _, _, err := rt.ResolveWords(execmodule.FrontendMCP, []string{"ops", "prod"}); err != nil {
		t.Errorf("MCP prod: %v", err)
	}
	if _, _, err := rt.ResolveWords(execmodule.FrontendCLI, []string{"ops", "status"}); err != nil {
		t.Errorf("CLI status: %v", err)
	}
}

func TestResolveWordsHiddenGroupAndModule(t *testing.T) {
	rt := exposeRT(t)
	tests := []struct {
		words []string
		path  []string
	}{
		{[]string{"ops", "grp"}, []string{"ops", "grp"}},
		{[]string{"ops", "grp", "x"}, []string{"ops", "grp"}},
		{[]string{"ops", "grp", "a"}, []string{"ops", "grp", "a"}}, // child of a hidden group is hidden too
		{[]string{"ng", "nocmd"}, []string{"ng", "nocmd"}},
		{[]string{"ng", "nocmd", "x"}, []string{"ng", "nocmd"}},
	}
	for _, tt := range tests {
		_, _, err := rt.ResolveWords(execmodule.FrontendCLI, tt.words)
		var ne *core.NotExposedError
		if !errors.As(err, &ne) || !slices.Equal(ne.Path, tt.path) {
			t.Errorf("ResolveWords(%q) err = %v, want NotExposedError at %q", tt.words, err, tt.path)
		}
		if core.ExitCode(err) != 1 {
			t.Errorf("ResolveWords(%q) exit code = %d, want 1", tt.words, core.ExitCode(err))
		}
		if _, _, err := rt.ResolveWords("", tt.words); errors.As(err, &ne) {
			t.Errorf("ResolveWords(\"\", %q) filtered: %v", tt.words, err)
		}
	}
}

func TestHiddenModuleStillChecksCollisions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ns", "a.yaml"), "expose: [mcp]\ncommands:\n  b:\n    cmd: echo\n")
	writeFile(t, filepath.Join(dir, "ns", "a", "b.yaml"), "cmd: echo\n")
	res, err := core.ListIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), nil, "ns", false)
	if err != nil {
		t.Fatal(err)
	}
	if names := childNames(res.Children); !slices.Equal(names, []string{"a"}) { // namespace a only
		t.Errorf("children = %q, want only the namespace a", names)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], `path "ns a b" is ambiguous`) {
		t.Errorf("Warnings = %q, want the collision of \"ns a b\"", res.Warnings)
	}
}

func TestNotExposedErrorFrontendNames(t *testing.T) {
	tests := []struct {
		f      execmodule.Frontend
		expose []execmodule.Frontend
		want   string
	}{
		{execmodule.FrontendMCP, []execmodule.Frontend{execmodule.FrontendCLI}, `command "futi a" is not available in MCP (expose: cli)`},
		{execmodule.FrontendHTTP, []execmodule.Frontend{execmodule.FrontendCLI, execmodule.FrontendMCP}, `command "futi a" is not available in HTTP (expose: cli, mcp)`},
	}
	for _, tt := range tests {
		err := &core.NotExposedError{Path: []string{"a"}, Frontend: tt.f, Expose: tt.expose}
		if err.Error() != tt.want {
			t.Errorf("Error() = %q, want %q", err.Error(), tt.want)
		}
	}
}

func TestListHidesNotExposed(t *testing.T) {
	src := exposeModules(t)

	res, err := core.ListIn(execmodule.FrontendCLI, src, nil, "ops", false)
	if err != nil {
		t.Fatal(err)
	}
	if names := commandNames(res); !slices.Equal(names, []string{"status"}) {
		t.Errorf("CLI commands = %q, want [status]", names)
	}

	res, err = core.ListIn(execmodule.FrontendCLI, src, nil, "ns", false)
	if err != nil {
		t.Fatal(err)
	}
	if names := childNames(res.Children); !slices.Equal(names, []string{"shown"}) {
		t.Errorf("CLI children = %q, want [shown]", names)
	}

	_, err = core.ListIn(execmodule.FrontendCLI, src, nil, "ns.hidden", false)
	var ne *core.NotExposedError
	if !errors.As(err, &ne) {
		t.Errorf("ns.hidden err = %v, want *NotExposedError", err)
	}
	_, err = core.ListIn(execmodule.FrontendCLI, src, nil, "ops.prod", false)
	if !errors.As(err, &ne) {
		t.Errorf("ops.prod err = %v, want *NotExposedError", err)
	}

	res, err = core.ListIn(execmodule.FrontendMCP, src, nil, "ops", false)
	if err != nil {
		t.Fatal(err)
	}
	if names := commandNames(res); !slices.Equal(names, []string{"grp", "prod", "status"}) {
		t.Errorf("MCP commands = %q, want [grp prod status]", names)
	}
	res, err = core.ListIn(execmodule.FrontendMCP, src, nil, "ns", false)
	if err != nil {
		t.Fatal(err)
	}
	if names := childNames(res.Children); !slices.Equal(names, []string{"hidden", "shown"}) {
		t.Errorf("MCP children = %q, want [hidden shown]", names)
	}
}

func TestHelpNotExposed(t *testing.T) {
	src := exposeModules(t)

	_, err := core.HelpIn(execmodule.FrontendCLI, src, nil, []string{"ops", "prod"})
	var ne *core.NotExposedError
	if !errors.As(err, &ne) {
		t.Fatalf("err = %v, want *NotExposedError", err)
	}

	doc, err := core.HelpIn(execmodule.FrontendCLI, src, nil, []string{"ns"})
	if err != nil {
		t.Fatal(err)
	}
	if names := childNames(doc.Children); !slices.Equal(names, []string{"shown"}) {
		t.Errorf("CLI help children = %q, want [shown]", names)
	}
}

func TestUnknownCommandHidesNotExposed(t *testing.T) {
	rt := exposeRT(t)

	_, _, err := rt.ResolveWords(execmodule.FrontendCLI, []string{"ops", "nope"})
	if err == nil || !strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "prod") {
		t.Errorf("CLI unknown: err = %v, want status without prod", err)
	}
	_, _, err = rt.ResolveWords(execmodule.FrontendCLI, []string{"ns"})
	if err == nil || !strings.Contains(err.Error(), "requires a subcommand") ||
		!strings.Contains(err.Error(), "shown") || strings.Contains(err.Error(), "hidden") {
		t.Errorf("CLI ns: err = %v, want shown without hidden", err)
	}

	_, _, err = rt.ResolveWords(execmodule.FrontendMCP, []string{"ops", "nope"})
	if err == nil || !strings.Contains(err.Error(), "status") || !strings.Contains(err.Error(), "prod") {
		t.Errorf("MCP unknown: err = %v, want status and prod", err)
	}
	_, _, err = rt.ResolveWords(execmodule.FrontendMCP, []string{"ns"})
	if err == nil || !strings.Contains(err.Error(), "shown") || !strings.Contains(err.Error(), "hidden") {
		t.Errorf("MCP ns: err = %v, want shown and hidden", err)
	}
}

func TestValidateSeesHidden(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ops.yaml"), `commands:
  status:
    cmd: echo status
  prod:
    expose: [mcp]
    cmd: "echo {{.nope}}"
`)
	src := discovery.NewGlobalSource(dir)
	rep, err := core.ValidateIn(src, "ops")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) == 0 {
		t.Fatal("Validate found no error in the hidden command")
	}
	mod, err := execmodule.ParseModule(filepath.Join(dir, "ops.yaml"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if help := execmodule.FormatCommandHelp("ops", &mod.Command, execmodule.FrontendCLI); strings.Contains(help, "prod") {
		t.Errorf("CLI help shows prod:\n%s", help)
	}
}

func commandNames(res core.ListResult) []string {
	var out []string
	for _, c := range res.Commands {
		out = append(out, c.Name)
	}
	return out
}

func childNames(cs []core.ChildEntry) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}
