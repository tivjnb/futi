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
`)
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
	if names := commandNames(res); !slices.Equal(names, []string{"prod", "status"}) {
		t.Errorf("MCP commands = %q, want [prod status]", names)
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
