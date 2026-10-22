package core_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

func TestHelpInEmptyPathIsUsage(t *testing.T) {
	doc, err := core.HelpIn(nil, nil, nil)
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
	doc, err := core.HelpIn(discovery.NewGlobalSource(dir), nil, []string{"db"})
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
	doc, err := core.HelpIn(discovery.NewGlobalSource(dir), nil, []string{"db", "status"})
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
	doc, err := core.HelpIn(discovery.NewGlobalSource(dir), nil, []string{"db", "migrate"})
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
	doc, err := core.HelpIn(discovery.NewGlobalSource(dir), nil, []string{"ns"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != core.HelpNamespace || len(doc.Children) != 1 || doc.Children[0].Name != "x" {
		t.Errorf("doc = %+v, want HelpNamespace with child x", doc)
	}
}

func TestHelpInNotFound(t *testing.T) {
	src := discovery.NewGlobalSource(t.TempDir())
	_, err := core.HelpIn(src, nil, []string{"a", "b"})
	if err == nil || err.Error() != `help: module not found for "a b"` {
		t.Fatalf("err = %v, want help: module not found for \"a b\"", err)
	}
	if !errors.Is(err, modulerunner.ErrModuleNotFound) {
		t.Errorf("err does not unwrap to modulerunner.ErrModuleNotFound: %v", err)
	}
	if !errors.Is(err, core.ErrNotFound) {
		t.Errorf("err is not classified ErrNotFound: %v", err)
	}
}

func TestHelpInNamespacePartial(t *testing.T) {
	dirA := t.TempDir()
	dirB := filepath.Join(t.TempDir(), "missing")
	writeFile(t, filepath.Join(dirA, "ns", "x.yaml"), "name: x\ncmd: echo x\n")
	cfg := &config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}}
	doc, err := core.HelpIn(discovery.NewCompositeSource(cfg), nil, []string{"ns"})
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
	return &execmodule.Module{Name: "db", Description: "PLAIN"}, nil
}

func (s *verifiedSpy) LoadModuleVerified(string, string) (*execmodule.Module, error) {
	s.verified++
	return &execmodule.Module{Name: "db", Description: "VERIFIED"}, nil
}

func TestHelpInModuleUsesVerifiedLoad(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.yaml"), "name: db\ncmd: echo\n")
	spy := &verifiedSpy{}
	doc, err := core.HelpIn(discovery.NewGlobalSource(dir), spy, []string{"db"})
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

func TestExecutableHelpIn(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.yaml"),
		"name: x\ncommands:\n  help:\n    cmd: echo custom\n")
	writeFile(t, filepath.Join(dir, "y.yaml"),
		"name: y\ncommands:\n  run:\n    cmd: echo run\n")
	writeFile(t, filepath.Join(dir, "help.yaml"), "name: help\ncmd: echo help\n")
	src := discovery.NewGlobalSource(dir)

	tests := []struct {
		name           string
		path           []string
		wantResolved   bool
		wantExecutable bool
	}{
		{"leaf help with cmd", []string{"x", "help"}, true, true},
		{"no help command", []string{"y", "help"}, true, false},
		{"module help with cmd", []string{"help"}, true, true},
		{"missing path", []string{"nope", "help"}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved, executable := core.ExecutableHelpIn(src, nil, tc.path)
			if resolved != tc.wantResolved || executable != tc.wantExecutable {
				t.Errorf("ExecutableHelpIn(%v) = (%v, %v), want (%v, %v)",
					tc.path, resolved, executable, tc.wantResolved, tc.wantExecutable)
			}
		})
	}
}
