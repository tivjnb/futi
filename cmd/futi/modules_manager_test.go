package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/modules"
)

func TestManagerInfoRejectsSlashName(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	_, err := newTestManager(t, io.Discard, nil).Info("dev/config")
	if err == nil || !strings.Contains(err.Error(), "write the namespace with a dot") {
		t.Fatalf("Info(dev/config) err = %v, want hint to write the namespace with a dot", err)
	}
}

func TestManagerListSorted(t *testing.T) {
	root := t.TempDir()
	meta := &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/x"}}
	installFixtureModule(t, root, "zeta", meta)
	installFixtureModule(t, root, "alpha", meta)
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	entries, err := newTestManager(t, io.Discard, nil).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if got := strings.Join(names, ","); got != "alpha,zeta" {
		t.Fatalf("List names = %q, want alpha,zeta", got)
	}
}

func TestCommandConfigFollowsPolicy(t *testing.T) {
	tests := []struct {
		name        string
		policy      core.ExecPolicy
		wantGroup   executor.GroupMode
		wantForward bool
	}{
		{"server", core.ServerExecPolicy, executor.GroupAlways, false},
		{"cli", core.CLIExecPolicy, executor.GroupInherit, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := core.NewRuntime(core.Options{ModulesDir: t.TempDir(), Exec: tc.policy})
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			in := strings.NewReader("")
			var out, errOut bytes.Buffer
			m := New(rt, modules.Streams{Out: &out, Err: &errOut, Stdin: in})

			cfg := m.commandConfig()
			if cfg.Group != tc.wantGroup || cfg.ForwardSignals != tc.wantForward {
				t.Errorf("Group = %v, ForwardSignals = %v; want %v, %v", cfg.Group, cfg.ForwardSignals, tc.wantGroup, tc.wantForward)
			}
			if cfg.Stdin != io.Reader(in) || cfg.Stdout != io.Writer(&out) || cfg.Stderr != io.Writer(&errOut) {
				t.Errorf("commandConfig streams do not match Streams")
			}
		})
	}
}
