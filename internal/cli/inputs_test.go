package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// inputsTestModules writes the k8s, files and docker modules of the scenario
// corpus used by the input grammar tests.
func inputsTestModules(t *testing.T) string {
	t.Helper()
	mods := t.TempDir()
	writeMainTestFile(t, filepath.Join(mods, "k8s.yaml"), `
commands:
  logs:
    description: Show pod logs
    args:
      - {name: pod, required: true, description: Pod name}
    flags:
      - {name: follow, type: bool, short: f}
      - {name: tail, type: int, default: "100"}
    cmd: "kubectl logs {{if .follow}}-f{{end}} --tail={{.tail}} -- {{.pod}}"
`)
	writeMainTestFile(t, filepath.Join(mods, "files.yaml"), `
commands:
  copy:
    args:
      - {name: src, required: true, variadic: true}
      - {name: dst, required: true}
    cmd: "cp {{.src}} {{.dst}}"
`)
	writeMainTestFile(t, filepath.Join(mods, "docker.yaml"), `
commands:
  run:
    args:
      - {name: image, required: true}
      - {name: docker_args, rest: true}
    cmd: "docker run {{.image}} {{.docker_args}}"
`)
	return mods
}

func TestRunInputs(t *testing.T) {
	mods := inputsTestModules(t)
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string // substring
	}{
		{"defaults", []string{"--dry-run", "k8s", "logs", "mypod"}, 0, "kubectl logs --tail=100 -- mypod\n", ""},
		{"flags and tail", []string{"--dry-run", "k8s", "logs", "mypod", "--follow", "--tail", "50"}, 0, "kubectl logs -f --tail=50 -- mypod\n", ""},
		{"flag before positional", []string{"--dry-run", "k8s", "logs", "--follow", "mypod"}, 0, "kubectl logs -f --tail=100 -- mypod\n", ""},
		{"short flag", []string{"--dry-run", "k8s", "logs", "-f", "mypod"}, 0, "kubectl logs -f --tail=100 -- mypod\n", ""},
		{"negative int value", []string{"--dry-run", "k8s", "logs", "mypod", "--tail", "-5"}, 0, "kubectl logs --tail=-5 -- mypod\n", ""},
		{"value after --", []string{"--dry-run", "k8s", "logs", "--", "--weird"}, 0, "kubectl logs --tail=100 -- --weird\n", ""},
		{"variadic and last arg", []string{"--dry-run", "files", "copy", "a.txt", "b.txt", "dir/"}, 0, "cp a.txt b.txt dir/\n", ""},
		{"rest keeps flags", []string{"--dry-run", "docker", "run", "nginx", "-it", "--rm", "-p", "80:80"}, 0, "docker run nginx -it --rm -p 80:80\n", ""},
		{"missing argument", []string{"k8s", "logs"}, 1, "", "missing argument <pod>"},
		{"extra argument", []string{"k8s", "logs", "mypod", "extra"}, 1, "", `unexpected argument "extra"`},
		{"bool flag does not take a word", []string{"k8s", "logs", "mypod", "--follow", "false"}, 1, "", "bool flag --follow takes no separate value; use --follow=false"},
		{"unknown flag", []string{"k8s", "logs", "mypod", "--unknown"}, 1, "", `unknown flag --unknown for "futi k8s logs"`},
		{"bad int", []string{"k8s", "logs", "mypod", "--tail", "many"}, 1, "", `invalid int value "many" for --tail`},
		{"dash value is refused", []string{"files", "copy", "--", "-rf", "dst"}, 1, "", `value "-rf" for <src> starts with "-" and would be read as an option by cp`},
		{"help flag", []string{"k8s", "logs", "mypod", "-h"}, 0, "Show pod logs\n\nusage: futi k8s logs [flags] <pod>\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runMainIn(t, mods, tt.args...)
			if code != tt.wantCode || !strings.HasPrefix(stdout, tt.wantStdout) || !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("code=%d stdout=%q stderr=%q\nwant %d, stdout starting %q, stderr containing %q",
					code, stdout, stderr, tt.wantCode, tt.wantStdout, tt.wantStderr)
			}
		})
	}
}

func TestHelpOutputGolden(t *testing.T) {
	mods := inputsTestModules(t)
	stdout, stderr, code := runMainIn(t, mods, "help", "k8s", "logs")
	want := `Show pod logs

usage: futi k8s logs [flags] <pod>
  pod  Pod name (required)
flags:
  -f, --follow
      --tail int  (default 100)
  -h, --help
`
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("code=%d stderr=%q stdout:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
}

func TestOldFormatModuleIsRejected(t *testing.T) {
	mods := t.TempDir()
	writeMainTestFile(t, filepath.Join(mods, "old.yaml"), "name: old\ncommands:\n  c:\n    cmd: echo\n    params:\n      - name: a\n")
	_, stderr, code := runMainIn(t, mods, "old", "c")
	if code != 1 || !strings.Contains(stderr, `"params" was replaced by "args"/"flags" (see README)`) {
		t.Errorf("code=%d stderr=%q, want 1 and the old-format message", code, stderr)
	}
}
