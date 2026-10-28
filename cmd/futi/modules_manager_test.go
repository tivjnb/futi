package main

import (
	"io"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/metayaml"
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
