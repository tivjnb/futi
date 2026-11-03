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
	"github.com/tivjnb/futi/internal/modulerunner"
)

func TestHelpInEmptyPathIsUsage(t *testing.T) {
	doc, err := core.HelpIn(execmodule.FrontendCLI, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != core.HelpUsage {
		t.Errorf("Kind = %v, want HelpUsage", doc.Kind)
	}
}

func TestHelpInModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ndescription: Database\ncmd: echo\n")
	doc, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), nil, []string{"db"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != core.HelpModule || doc.Module == nil || doc.Module.Description != "Database" {
		t.Errorf("doc = %+v, want HelpModule with description Database", doc)
	}
}

func TestHelpInLeafCommand(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"),
		"name: db\ncommands:\n  status:\n    description: Show status\n    cmd: echo ok\n")
	doc, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), nil, []string{"db", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != core.HelpCommand || doc.Command == nil || doc.Command.Description != "Show status" {
		t.Errorf("doc = %+v, want HelpCommand with description Show status", doc)
	}
}

func TestHelpInBranchIsSyntheticModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"),
		"name: db\ndescription: Database\ncommands:\n  migrate:\n    description: Migrations\n    commands:\n      up:\n        cmd: echo up\n")
	doc, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), nil, []string{"db", "migrate"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != core.HelpModule || doc.Module == nil {
		t.Fatalf("doc = %+v, want HelpModule", doc)
	}
	if doc.Module.Name != "db" || doc.Module.Description != "Migrations" {
		t.Errorf("Module = {Name:%q Description:%q}, want {db Migrations}", doc.Module.Name, doc.Module.Description)
	}
	var cmds []string
	for n := range doc.Module.Commands {
		cmds = append(cmds, n)
	}
	if !reflect.DeepEqual(cmds, []string{"up"}) {
		t.Errorf("Commands = %v, want [up]", cmds)
	}
}

func TestHelpInNamespaceChildren(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ns", "x.yaml"), "name: x\ncmd: echo x\n")
	doc, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), nil, []string{"ns"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != core.HelpNamespace || len(doc.Children) != 1 || doc.Children[0].Name != "x" {
		t.Errorf("doc = %+v, want HelpNamespace with child x", doc)
	}
}

func TestHelpInNotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ncommands:\n  up:\n    cmd: echo up\n")
	src := discovery.NewGlobalSource(dir)
	tests := []struct {
		path []string
		want string
		is   error
	}{
		{[]string{"a", "b"}, `unknown command "a" for "futi"`, modulerunner.ErrModuleNotFound},
		{[]string{"db", "up", "extra"}, `unknown command "extra" for "futi db up"`, execmodule.ErrCommandNotFound},
		{[]string{"db", "down"}, `unknown command "down" for "futi db" (available: up)`, execmodule.ErrCommandNotFound},
	}
	for _, tt := range tests {
		_, err := core.HelpIn(execmodule.FrontendCLI, src, nil, tt.path)
		if err == nil || err.Error() != tt.want {
			t.Errorf("HelpIn(%q) err = %v, want %q", tt.path, err, tt.want)
			continue
		}
		if !errors.Is(err, tt.is) || !errors.Is(err, core.ErrNotFound) {
			t.Errorf("HelpIn(%q) err = %v, want %v classified ErrNotFound", tt.path, err, tt.is)
		}
	}
}

func TestHelpInDottedPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ncommands:\n  up:\n    description: Apply\n    cmd: echo up\n")
	doc, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), nil, []string{"db.up"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != core.HelpCommand || doc.Command.Description != "Apply" || !reflect.DeepEqual(doc.Path, []string{"db", "up"}) {
		t.Errorf("doc = %+v, want HelpCommand of db up", doc)
	}
}

func TestHelpInCollision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev.yaml"), "name: dev\ncommands:\n  deploy:\n    cmd: echo a\n")
	writeFile(t, filepath.Join(dir, "dev", "deploy.yaml"), "cmd: echo b\n")
	_, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), nil, []string{"dev", "deploy"})
	var ce *modulerunner.CollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *CollisionError", err)
	}
	if strings.HasPrefix(err.Error(), "help:") {
		t.Errorf("err = %q, want the collision text without a help: prefix", err)
	}
}

func TestHelpInNamespacePartial(t *testing.T) {
	dirA := t.TempDir()
	dirB := filepath.Join(t.TempDir(), "missing")
	writeFile(t, filepath.Join(dirA, "ns", "x.yaml"), "name: x\ncmd: echo x\n")
	cfg := &config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}}
	doc, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewCompositeSource(cfg), nil, []string{"ns"})
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *discovery.PartialError", err)
	}
	if errors.Is(err, core.ErrNotFound) {
		t.Errorf("partial error must not be classified: %v", err)
	}
	if doc.Kind != core.HelpNamespace || len(doc.Children) == 0 {
		t.Errorf("doc = %+v, want HelpNamespace with children", doc)
	}
}

// verifiedSpy returns different modules from LoadModule and LoadModuleVerified.
type verifiedSpy struct{ verified, plain int }

func (s *verifiedSpy) LoadModule(string, string) (*execmodule.Module, error) {
	s.plain++
	return &execmodule.Module{Name: "db", Command: execmodule.Command{Description: "PLAIN"}}, nil
}

func (s *verifiedSpy) LoadModuleVerified(string, string) (*execmodule.Module, error) {
	s.verified++
	return &execmodule.Module{Name: "db", Command: execmodule.Command{Description: "VERIFIED"}}, nil
}

func TestHelpInModuleUsesVerifiedLoad(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ncmd: echo\n")
	spy := &verifiedSpy{}
	doc, err := core.HelpIn(execmodule.FrontendCLI, discovery.NewGlobalSource(dir), spy, []string{"db"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Module == nil || doc.Module.Description != "VERIFIED" {
		t.Errorf("doc = %+v, want description VERIFIED", doc)
	}
	if spy.verified != 1 || spy.plain != 0 {
		t.Errorf("verified=%d plain=%d, want 1, 0", spy.verified, spy.plain)
	}
}

func TestRuntimeHelpUsesRuntimeLoader(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ncmd: echo\n")
	spy := &verifiedSpy{}
	opts := core.Options{ModulesDir: dir}
	core.SetLoader(&opts, spy)
	rt, err := core.NewRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := rt.Help(execmodule.FrontendCLI, []string{"db"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Module == nil || doc.Module.Description != "VERIFIED" || spy.verified != 1 {
		t.Errorf("doc = %+v verified=%d, want runtime loader reached", doc, spy.verified)
	}
}

func TestRuntimeHelpPropagatesSourceError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "futi.yaml"), "name: [\n  invalid")
	_, err := newRT(t, dir).Help(execmodule.FrontendCLI, []string{"db"})
	if err == nil || !strings.HasPrefix(err.Error(), "local source: ") {
		t.Fatalf("err = %v, want prefix %q", err, "local source: ")
	}
}
