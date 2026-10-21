package core_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/modulerunner"
)

func TestResolveTarget(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "m.yaml")
	writeFile(t, manifest, "name: mod\ncommands:\n  c:\n    cmd: echo hi\n")
	t.Chdir(dir)
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	tgt, err := rt.Resolve("m", "c")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := tgt.Command().Cmd; got != "echo hi" {
		t.Errorf("Command().Cmd = %q, want %q", got, "echo hi")
	}
	if got := tgt.ModuleName(); got != "mod" {
		t.Errorf("ModuleName() = %q, want %q", got, "mod")
	}
	if got := tgt.Manifest(); got != manifest {
		t.Errorf("Manifest() = %q, want %q", got, manifest)
	}
}

func TestResolveErrorClasses(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "nodef.yaml"), "name: nodef\ncommands:\n  c:\n    cmd: echo hi\n")
	writeFile(t, filepath.Join(dir, "group.yaml"), "name: group\ncommands:\n  g:\n    commands:\n      leaf:\n        cmd: echo hi\n")
	rt, err := core.NewRuntime(core.Options{ModulesDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	src, err := rt.Source()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, ns, cmd string
		want          error
	}{
		{"missing module", "nope", "c", core.ErrNotFound},
		{"missing command", "nodef", "zzz", core.ErrNotFound},
		{"no default command", "nodef", "", core.ErrInvalidInput},
		{"invalid namespace", "a..b", "c", core.ErrInvalidInput},
		{"group requires subcommand", "group", "g", core.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rt.Resolve(tc.ns, tc.cmd)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Resolve(%q, %q) err = %v, want class %v", tc.ns, tc.cmd, err, tc.want)
			}
			_, base := modulerunner.Resolve(src, nil, tc.ns, tc.cmd)
			if base == nil || err.Error() != base.Error() {
				t.Errorf("text = %q, want %v", err, base)
			}
		})
	}
}
