package core_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

func names(es []core.CommandEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestListInModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"),
		"name: db\ncmd: echo\ncommands:\n  b:\n    description: Bee\n    cmd: echo b\n  a:\n    description: Ay\n    cmd: echo a\n")
	res, err := core.ListIn(discovery.NewGlobalSource(dir), nil, "db", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != core.ListModule || !res.HasDefault {
		t.Errorf("Kind=%v HasDefault=%v, want ListModule, true", res.Kind, res.HasDefault)
	}
	want := []core.CommandEntry{{Name: "a", Description: "Ay"}, {Name: "b", Description: "Bee"}}
	if !reflect.DeepEqual(res.Commands, want) {
		t.Errorf("Commands = %v, want %v", res.Commands, want)
	}
	if res.Manifest != filepath.Join(dir, "db.yaml") {
		t.Errorf("Manifest = %q", res.Manifest)
	}
}

func TestListInSubcommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"),
		"name: db\ncommands:\n  migrate:\n    commands:\n      up:\n        cmd: echo up\n      down:\n        cmd: echo down\n")
	res, err := core.ListIn(discovery.NewGlobalSource(dir), nil, "db.migrate", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != core.ListSubcommands || res.HasDefault {
		t.Errorf("Kind=%v HasDefault=%v, want ListSubcommands, false", res.Kind, res.HasDefault)
	}
	if got := names(res.Commands); !reflect.DeepEqual(got, []string{"down", "up"}) {
		t.Errorf("names = %v", got)
	}
}

func TestListInLeafHasNoSubcommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ncommands:\n  status:\n    cmd: echo ok\n")
	_, err := core.ListIn(discovery.NewGlobalSource(dir), nil, "db.status", false)
	want := `list: command "status" has no subcommands`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestListInNamespaceChildren(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.yaml"), "name: x\ndescription: X\ncmd: echo x\n")
	writeFile(t, filepath.Join(dir, "broken.yaml"), "name: [unclosed\n")
	writeFile(t, filepath.Join(dir, "ns", "y.yaml"), "name: y\ncmd: echo y\n")
	res, err := core.ListIn(discovery.NewGlobalSource(dir), nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != core.ListNamespace {
		t.Fatalf("Kind = %v", res.Kind)
	}
	byName := map[string]core.ChildEntry{}
	for _, c := range res.Children {
		byName[c.Name] = c
	}
	if c := byName["x"]; !c.IsModule || c.Description != "X" {
		t.Errorf("x = %+v", c)
	}
	if c := byName["broken"]; c.LoadErr == nil || c.Description != "" {
		t.Errorf("broken = %+v", c)
	}
	if c, ok := byName["ns"]; !ok || c.IsModule {
		t.Errorf("ns = %+v (ok=%v)", c, ok)
	}
}

func TestListInNotFoundIsClassified(t *testing.T) {
	dir := t.TempDir()
	_, err := core.ListIn(discovery.NewGlobalSource(dir), nil, "nope", false)
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if n := strings.Count(err.Error(), "list: "); n != 1 || !strings.HasPrefix(err.Error(), "list: ") {
		t.Errorf("err text = %q, want single list: prefix", err)
	}
}

func TestListInPartialNotClassified(t *testing.T) {
	dirA := t.TempDir()
	dirB := filepath.Join(t.TempDir(), "missing")
	writeFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ncmd: echo x\n")
	cfg := &config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}}
	res, err := core.ListIn(discovery.NewCompositeSource(cfg), nil, "", false)
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *discovery.PartialError", err)
	}
	if errors.Is(err, core.ErrNotFound) {
		t.Errorf("partial error must not be classified: %v", err)
	}
	found := false
	for _, c := range res.Children {
		found = found || c.Name == "x"
	}
	if !found {
		t.Errorf("Children = %+v, want x", res.Children)
	}
}

func TestDescribeChildrenSkipsConflicts(t *testing.T) {
	spy := &stubLoader{mod: &execmodule.Module{Description: "never"}}
	got := core.DescribeChildren([]discovery.Child{
		{Name: "x", Type: "module", Conflict: true, ManifestPath: "/d/x.yaml"},
	}, spy)
	if spy.calls != 0 {
		t.Errorf("loader calls = %d, want 0", spy.calls)
	}
	if len(got) != 1 || !got[0].Conflict || got[0].LoadErr != nil || got[0].Description != "" {
		t.Errorf("got = %+v", got)
	}
}

func TestRuntimeListUsesRuntimeLoader(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ncmd: echo\n")
	spy := &stubLoader{mod: &execmodule.Module{Name: "db", Commands: map[string]*execmodule.Command{
		"spied": {Description: "from spy"},
	}}}
	opts := core.Options{ModulesDir: dir}
	core.SetLoader(&opts, spy)
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rt.List("db", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(res.Commands); !reflect.DeepEqual(got, []string{"spied"}) {
		t.Errorf("names = %v", got)
	}
	if spy.calls != 1 {
		t.Errorf("calls = %d, want 1", spy.calls)
	}
}

func TestListInConflictSources(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ncmd: echo a\n")
	writeFile(t, filepath.Join(dirB, "x.yaml"), "name: x\ncmd: echo b\n")
	cfg := &config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}}
	spy := &stubLoader{mod: &execmodule.Module{Description: "never"}}

	res, err := core.ListIn(discovery.NewCompositeSource(cfg), spy, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Children) != 1 {
		t.Fatalf("Children = %+v, want one", res.Children)
	}
	c := res.Children[0]
	if c.Name != "x" || !c.Conflict || !reflect.DeepEqual(c.ConflictSources, []string{dirA, dirB}) {
		t.Errorf("child = %+v, want conflict x with sources [%s %s]", c, dirA, dirB)
	}
	if c.LoadErr != nil || c.Description != "" || spy.calls != 0 {
		t.Errorf("conflict must not load: LoadErr=%v Description=%q calls=%d", c.LoadErr, c.Description, spy.calls)
	}
}

func TestListInConflictFileVsConfigYaml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.yaml"), "name: x\ncmd: echo a\n")
	writeFile(t, filepath.Join(dir, "x", "config.yaml"), "name: x\ncmd: echo b\n")
	spy := &stubLoader{mod: &execmodule.Module{Description: "never"}}

	res, err := core.ListIn(discovery.NewGlobalSource(dir), spy, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Children) != 1 {
		t.Fatalf("Children = %+v, want one", res.Children)
	}
	c := res.Children[0]
	if c.Name != "x" || !c.Conflict || len(c.ConflictSources) != 0 {
		t.Errorf("child = %+v, want conflict x without ConflictSources", c)
	}
	if spy.calls != 0 {
		t.Errorf("loader calls = %d, want 0", spy.calls)
	}
}
