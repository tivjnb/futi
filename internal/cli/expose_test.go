package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNotExposedSameTextEverywhere(t *testing.T) {
	mods := t.TempDir()
	writeMainTestFile(t, filepath.Join(mods, "ops.yaml"), `expose: [cli, mcp]
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
	if err := os.MkdirAll(filepath.Join(mods, "ng"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(mods, "ng", "nocmd.yaml"), "expose: [mcp]\ncommands:\n  a:\n    cmd: echo a\n")
	want := "command \"futi ops prod\" is not available in the CLI (expose: mcp)\n"
	grp := "command \"futi ops grp\" is not available in the CLI (expose: mcp)\n"
	nocmd := "command \"futi ng nocmd\" is not available in the CLI (expose: mcp)\n"
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"ops", "prod"}, want},
		{[]string{"help", "ops", "prod"}, want},
		{[]string{"ops", "prod", "-h"}, want},
		{[]string{"list", "ops.prod"}, want},
		{[]string{"ops", "grp"}, grp},
		{[]string{"ops", "grp", "x"}, grp},
		{[]string{"help", "ops", "grp"}, grp},
		{[]string{"ops", "grp", "-h"}, grp},
		{[]string{"list", "ops.grp"}, grp},
		{[]string{"ng", "nocmd"}, nocmd},
		{[]string{"ng", "nocmd", "x"}, nocmd},
		{[]string{"help", "ng", "nocmd"}, nocmd},
		{[]string{"list", "ng.nocmd"}, nocmd},
	}
	for _, tt := range tests {
		stdout, stderr, code := runMainIn(t, mods, tt.args...)
		if code != 1 || stderr != tt.want || stdout != "" {
			t.Errorf("futi %v: code=%d stdout=%q stderr=%q, want code 1, stderr %q", tt.args, code, stdout, stderr, tt.want)
		}
	}
}
