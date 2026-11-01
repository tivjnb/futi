package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runMainIn runs Main in-process with modules in mods and an isolated HOME,
// cache and cwd.
func runMainIn(t *testing.T, mods string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", mods)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	code = Main(append([]string{"futi"}, args...), strings.NewReader(""), &out, &errb, "")
	return out.String(), errb.String(), code
}

// pathTestModules writes the modules used by the path grammar tests.
func pathTestModules(t *testing.T) string {
	t.Helper()
	mods := t.TempDir()
	writeMainTestFile(t, filepath.Join(mods, "docker.yaml"), `description: Docker
commands:
  build:
    description: Build an image
    cmd: echo build
  run:
    cmd: echo run
`)
	writeMainTestFile(t, filepath.Join(mods, "db.yaml"), `commands:
  migrate:
    cmd: echo migrate
    commands:
      up:
        cmd: echo up
`)
	writeMainTestFile(t, filepath.Join(mods, "git.yaml"), `commands:
  list:
    cmd: echo user-list
  help:
    cmd: echo user-help
`)
	return mods
}

func TestRunNoSubcommandMessages(t *testing.T) {
	mods := pathTestModules(t)
	tests := []struct {
		args       []string
		wantStderr string
	}{
		{[]string{"docker", "x"}, "unknown command \"x\" for \"futi docker\" (available: build, run)\n"},
		{[]string{"docker"}, "\"futi docker\" requires a subcommand (available: build, run)\n"},
		{[]string{"db", "--verbose", "migrate", "up"}, "\"futi db\" requires a subcommand (available: migrate) (put the full command path before flags)\n"},
		{[]string{"nope"}, "unknown command \"nope\" for \"futi\"\n"},
		{[]string{"docker", "list"}, "unknown command \"list\" for \"futi docker\" (available: build, run)\n"},
		{[]string{"docker", "help"}, "unknown command \"help\" for \"futi docker\" (available: build, run)\n"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			stdout, stderr, code := runMainIn(t, mods, tt.args...)
			if code != 1 || stdout != "" || stderr != tt.wantStderr {
				t.Errorf("code=%d stdout=%q stderr=%q\nwant 1, \"\", %q", code, stdout, stderr, tt.wantStderr)
			}
		})
	}
}

func TestRunParentFlagNotInherited(t *testing.T) {
	mods := t.TempDir()
	writeMainTestFile(t, filepath.Join(mods, "db.yaml"), `cmd: echo db {{if .verbose}}v{{end}}
flags:
  - name: verbose
    type: bool
commands:
  migrate:
    cmd: echo migrate
`)
	_, stderr, code := runMainIn(t, mods, "db", "migrate", "--verbose")
	if code != 1 || stderr != "unknown flag --verbose for \"futi db migrate\"\n" {
		t.Errorf("code=%d stderr=%q, want 1 and unknown flag --verbose for \"futi db migrate\"", code, stderr)
	}
}

func TestRunTrailingHelpAndListAreUserCommands(t *testing.T) {
	mods := pathTestModules(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"git", "list"}, "user-list\n"},
		{[]string{"git", "help"}, "user-help\n"},
		{[]string{"db", "migrate", "up"}, "up\n"},
	} {
		stdout, stderr, code := runMainIn(t, mods, tt.args...)
		if code != 0 || stdout != tt.want {
			t.Errorf("%q: code=%d stdout=%q stderr=%q, want 0, %q", tt.args, code, stdout, stderr, tt.want)
		}
	}
}

func TestRunCollision(t *testing.T) {
	mods := t.TempDir()
	writeMainTestFile(t, filepath.Join(mods, "dev.yaml"), "commands:\n  deploy:\n    cmd: echo a\n")
	if err := os.Mkdir(filepath.Join(mods, "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(mods, "dev", "deploy.yaml"), "cmd: echo b\n")
	stdout, stderr, code := runMainIn(t, mods, "dev", "deploy")
	want := "path \"dev deploy\" is ambiguous: command \"deploy\" of module dev (" + filepath.Join(mods, "dev.yaml") +
		") and module dev.deploy (" + filepath.Join(mods, "dev", "deploy.yaml") + ")\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Errorf("code=%d stdout=%q stderr=%q\nwant 1, \"\", %q", code, stdout, stderr, want)
	}
}

func TestHelpFirstWord(t *testing.T) {
	mods := pathTestModules(t)
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // exact
	}{
		{"command help", []string{"help", "docker", "build"}, 0, "Build an image", ""},
		{"dotted path", []string{"help", "docker.build"}, 0, "Build an image", ""},
		{"module help", []string{"help", "docker"}, 0, "build", ""},
		{"user command named help is still described", []string{"help", "git", "help"}, 0, "usage: futi git help", ""},
		{"extra word", []string{"help", "docker", "build", "extra"}, 1, "", "unknown command \"extra\" for \"futi docker build\"\n"},
		{"unknown module", []string{"help", "nope"}, 1, "", "unknown command \"nope\" for \"futi\"\n"},
		{"usage", []string{"help"}, 0, "USAGE", ""},
		{"-h after a help path", []string{"help", "docker", "build", "-h"}, 0, "Build an image", ""},
		{"--help after a help module", []string{"help", "docker", "--help"}, 0, "build", ""},
		{"help --help is futi help", []string{"help", "--help"}, 0, "USAGE", ""},
		{"-h after path", []string{"docker", "build", "-h"}, 0, "Build an image", ""},
		{"-h after a node without cmd", []string{"docker", "-h"}, 0, "build", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runMainIn(t, mods, tt.args...)
			if code != tt.wantCode || !strings.Contains(stdout, tt.wantStdout) || stderr != tt.wantStderr {
				t.Errorf("code=%d stdout=%q stderr=%q\nwant %d, stdout with %q, stderr %q",
					code, stdout, stderr, tt.wantCode, tt.wantStdout, tt.wantStderr)
			}
		})
	}
}

func TestListFirstWordPath(t *testing.T) {
	mods := pathTestModules(t)
	for _, args := range [][]string{{"list", "docker"}, {"list", "docker", "-v"}} {
		stdout, stderr, code := runMainIn(t, mods, args...)
		if code != 0 || !strings.Contains(stdout, "build") || !strings.Contains(stdout, "run") {
			t.Errorf("%q: code=%d stdout=%q stderr=%q, want listing of docker", args, code, stdout, stderr)
		}
	}
}
