package cli

import (
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
`)
	want := "command \"futi ops prod\" is not available in the CLI (expose: mcp)\n"
	for _, args := range [][]string{
		{"ops", "prod"},
		{"help", "ops", "prod"},
		{"ops", "prod", "-h"},
		{"list", "ops.prod"},
	} {
		stdout, stderr, code := runMainIn(t, mods, args...)
		if code != 1 || stderr != want || stdout != "" {
			t.Errorf("futi %v: code=%d stdout=%q stderr=%q, want code 1, stderr %q", args, code, stdout, stderr, want)
		}
	}
}
