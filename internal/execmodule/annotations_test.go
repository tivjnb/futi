package execmodule

import (
	"bytes"
	"encoding/gob"
	"slices"
	"testing"
)

func annParse(t *testing.T, yamlText string) (*Module, []error) {
	t.Helper()
	m, err := ParseManifest([]byte(yamlText), "m.yaml", "/m")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	errs, _ := m.Validate()
	return m, errs
}

func TestAnnotationsParse(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string // exact error text; empty means no errors
	}{
		{"readonly on group", "commands:\n  db:\n    readonly: true\n    commands:\n      a: {cmd: x}\n", `command "db": readonly applies only to a command with cmd`},
		{"destructive on group", "commands:\n  db:\n    destructive: true\n    commands:\n      a: {cmd: x}\n", `command "db": destructive applies only to a command with cmd`},
		{"idempotent on group", "commands:\n  db:\n    idempotent: false\n    commands:\n      a: {cmd: x}\n", `command "db": idempotent applies only to a command with cmd`},
		{"readonly on root without cmd", "readonly: true\ncommands:\n  a: {cmd: x}\n", `command "(default)": readonly applies only to a command with cmd`},
		{"readonly and destructive", "commands:\n  drop:\n    cmd: x\n    readonly: true\n    destructive: true\n", `command "drop": readonly and destructive are mutually exclusive`},
		{"readonly and destructive false", "commands:\n  a:\n    cmd: x\n    readonly: true\n    destructive: false\n", ""},
		{"readonly and idempotent", "commands:\n  a:\n    cmd: x\n    readonly: true\n    idempotent: true\n", ""},
		{"unknown expose", "commands:\n  x:\n    cmd: x\n    expose: [mpc]\n", `command "x": unknown expose value "mpc" (want cli, mcp or http)`},
		{"duplicate expose", "commands:\n  x:\n    cmd: x\n    expose: [cli, cli]\n", `command "x": duplicate expose value "cli"`},
		{"empty expose", "commands:\n  x:\n    cmd: x\n    expose: []\n", `command "x": expose is empty: the command is unreachable`},
		{"widens root", "expose: [cli]\ncmd: r\ncommands:\n  sub:\n    cmd: x\n    expose: [cli, mcp]\n", `command "sub": expose widens the parent: "mcp" is not exposed by "(default)"`},
		{"widens group", "commands:\n  db:\n    expose: [cli]\n    commands:\n      sub:\n        cmd: x\n        expose: [http]\n", `command "db.sub": expose widens the parent: "http" is not exposed by "db"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := annParse(t, tc.yaml)
			if tc.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("errors = %v, want none", errs)
				}
				return
			}
			if len(errs) != 1 || errs[0].Error() != tc.wantErr {
				t.Fatalf("errors = %v, want exactly [%s]", errs, tc.wantErr)
			}
		})
	}
}

func TestAnnotationsValues(t *testing.T) {
	m, errs := annParse(t, `cmd: r
destructive: true
commands:
  a:
    cmd: x
    destructive: true
    idempotent: false
`)
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	a := m.Commands["a"]
	if a.Destructive != TriTrue || a.ReadOnly != TriUnset || a.Idempotent != TriFalse {
		t.Errorf("sub = %v/%v/%v, want TriTrue/TriUnset/TriFalse", a.Destructive, a.ReadOnly, a.Idempotent)
	}
	if !a.IsDestructive() || a.Idempotent.IsTrue() {
		t.Errorf("IsDestructive/IsTrue wrong for sub")
	}
	if m.Destructive != TriTrue || m.ReadOnly != TriUnset {
		t.Errorf("root = %v/%v, want TriTrue/TriUnset", m.Destructive, m.ReadOnly)
	}
}

func TestAnnotationsExposeInheritance(t *testing.T) {
	m, errs := annParse(t, `expose: [cli, mcp]
commands:
  grp:
    commands:
      leaf: {cmd: x}
      narrow: {cmd: x, expose: [mcp]}
`)
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	if got := m.Commands["grp"].Commands["leaf"].Expose; !slices.Equal(got, []Frontend{FrontendCLI, FrontendMCP}) {
		t.Errorf("leaf Expose = %v, want [cli mcp]", got)
	}
	if got := m.Commands["grp"].Commands["narrow"].Expose; !slices.Equal(got, []Frontend{FrontendMCP}) {
		t.Errorf("narrow Expose = %v, want [mcp]", got)
	}

	m, errs = annParse(t, "commands:\n  a: {cmd: x}\n")
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	if got := m.Commands["a"].Expose; !slices.Equal(got, []Frontend{FrontendCLI, FrontendMCP, FrontendHTTP}) {
		t.Errorf("default Expose = %v, want [cli mcp http]", got)
	}
}

func TestExposedTo(t *testing.T) {
	m, errs := annParse(t, "commands:\n  a: {cmd: x, expose: [mcp]}\n")
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	a := m.Commands["a"]
	if a.ExposedTo(FrontendCLI) {
		t.Error("ExposedTo(cli) = true, want false")
	}
	if !a.ExposedTo(FrontendMCP) {
		t.Error("ExposedTo(mcp) = false, want true")
	}
	if !(&Command{}).ExposedTo(FrontendCLI) {
		t.Error("zero Command ExposedTo(cli) = false, want true")
	}
}

func TestAnnotationsGobRoundTrip(t *testing.T) {
	m, errs := annParse(t, "cmd: r\ncommands:\n  a:\n    cmd: x\n    idempotent: false\n    destructive: true\n    expose: [cli]\n")
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(m); err != nil {
		t.Fatal(err)
	}
	var got Module
	if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatal(err)
	}
	a := got.Commands["a"]
	if a.Idempotent != TriFalse || a.Destructive != TriTrue || !slices.Equal(a.Expose, []Frontend{FrontendCLI}) {
		t.Errorf("after gob: %v/%v/%v, want TriFalse/TriTrue/[cli]", a.Idempotent, a.Destructive, a.Expose)
	}
}
