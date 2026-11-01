package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/argvtmpl"
	"github.com/tivjnb/futi/internal/inputs"
)

// scenarioModules copies the scenario fixtures (testdata/scenarios, except
// the collision directory) into a fresh modules directory.
func scenarioModules(t *testing.T) string {
	t.Helper()
	mods := t.TempDir()
	copyScenarioDir(t, filepath.Join("testdata", "scenarios"), mods, "collision")
	return mods
}

func copyScenarioDir(t *testing.T, src, dst string, skip ...string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(skip) > 0 && e.Name() == skip[0] {
			continue
		}
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(to, 0o755); err != nil {
				t.Fatal(err)
			}
			copyScenarioDir(t, from, to)
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		writeMainTestFile(t, to, string(data))
	}
}

// TestScenarios runs the end-to-end scenarios S1-S28 of the args design
// against the built binary. Each case is either a dry-run (stdout is the
// shell-joined argv) or an expected error.
func TestScenarios(t *testing.T) {
	mods := scenarioModules(t)
	type tc struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // exact
		wantStderr string // substring
	}
	tests := []tc{
		{"S1", []string{"--dry-run", "k8s", "logs", "mypod"}, 0, "kubectl logs --tail=100 -- mypod\n", ""},
		{"S2", []string{"--dry-run", "k8s", "logs", "mypod", "--follow", "--tail", "50"}, 0, "kubectl logs -f --tail=50 -- mypod\n", ""},
		{"S3 flag before positional", []string{"--dry-run", "k8s", "logs", "--follow", "mypod"}, 0, "kubectl logs -f --tail=100 -- mypod\n", ""},
		{"S4 short before", []string{"--dry-run", "k8s", "logs", "-f", "mypod"}, 0, "kubectl logs -f --tail=100 -- mypod\n", ""},
		{"S4 short after", []string{"--dry-run", "k8s", "logs", "mypod", "-f"}, 0, "kubectl logs -f --tail=100 -- mypod\n", ""},
		{"S5 separate bool value", []string{"--dry-run", "k8s", "logs", "mypod", "--follow", "false"}, 1, "", "bool flag --follow takes no separate value; use --follow=false"},
		{"S5 equals false", []string{"--dry-run", "k8s", "logs", "mypod", "--follow=false"}, 0, "kubectl logs --tail=100 -- mypod\n", ""},
		{"S6 missing positional", []string{"k8s", "logs"}, 1, "", "missing argument <pod>"},
		{"S7 extra positional", []string{"k8s", "logs", "mypod", "extra"}, 1, "", `unexpected argument "extra"`},
		{"S8 dash value after --", []string{"--dry-run", "k8s", "logs", "--", "--weird-pod-name"}, 0, "kubectl logs --tail=100 -- --weird-pod-name\n", ""},
		{"S9 negative int", []string{"--dry-run", "k8s", "logs", "mypod", "--tail", "-5"}, 0, "kubectl logs --tail=-5 -- mypod\n", ""},
		{"S10 variadic and last", []string{"--dry-run", "files", "copy", "a.txt", "b.txt", "dir/"}, 0, "cp -- a.txt b.txt dir/\n", ""},
		{"S11 grep with paths", []string{"--dry-run", "files", "grep", "-i", "TODO", "src", "lib"}, 0, "grep -i -- TODO src lib\n", ""},
		{"S11 grep without paths", []string{"--dry-run", "files", "grep", "TODO"}, 0, "grep -- TODO\n", ""},
		{"S12 pattern with dash", []string{"--dry-run", "files", "grep", "--", "-v", "src"}, 0, "grep -- -v src\n", ""},
		{"S13 module default cmd", []string{"--dry-run", "docker"}, 0, "docker ps\n", ""},
		{"S13 command", []string{"--dry-run", "docker", "build", "--tag", "x"}, 0, "docker build --file=Dockerfile --tag=x -- .\n", ""},
		{"S14 context given", []string{"--dry-run", "docker", "build", "--tag", "x", "./ctx"}, 0, "docker build --file=Dockerfile --tag=x -- ./ctx\n", ""},
		{"S14 context default", []string{"--dry-run", "docker", "build", "--tag", "x"}, 0, "docker build --file=Dockerfile --tag=x -- .\n", ""},
		{"S15 rest passes flags", []string{"--dry-run", "docker", "run", "nginx", "-it", "--rm", "-p", "80:80"}, 0, "docker run nginx -it --rm -p 80:80\n", ""},
		{"S16 echo words", []string{"--dry-run", "echo", "hello", "world"}, 0, "echo hello world\n", ""},
		{"S16 echo build is a value", []string{"--dry-run", "echo", "build"}, 0, "echo build\n", ""},
		{"S17 user help command", []string{"--dry-run", "git", "help", "commit"}, 0, "git help commit\n", ""},
		{"S17 commit help is an extra argument", []string{"git", "commit", "help"}, 1, "", `unexpected argument "help"`},
		{"S19 int positional", []string{"--dry-run", "db", "migrate", "3"}, 0, "db-migrate --steps=3\n", ""},
		{"S19 subcommand", []string{"--dry-run", "db", "migrate", "up"}, 0, "db-migrate up\n", ""},
		{"S19 flag before makes it an input", []string{"db", "migrate", "--steps", "1", "up"}, 1, "", "unknown flag --steps"},
		{"S20 short and long", []string{"--dry-run", "git", "commit", "-m", "fix: x", "--amend"}, 0, "git commit --amend '--message=fix: x'\n", ""},
		{"S20 equals", []string{"--dry-run", "git", "commit", "--message=fix: x"}, 0, "git commit '--message=fix: x'\n", ""},
		{"S21 flag without value", []string{"git", "commit", "-m"}, 1, "", "flag --message requires a value"},
		{"S22 last wins", []string{"--dry-run", "k8s", "logs", "mypod", "--follow", "--follow=false"}, 0, "kubectl logs --tail=100 -- mypod\n", ""},
		{"S23 unknown flag", []string{"k8s", "logs", "mypod", "--unknown"}, 1, "", `unknown flag --unknown for "futi k8s logs"`},
		{"S24 global dry-run before path", []string{"--dry-run", "k8s", "logs", "mypod", "-f"}, 0, "kubectl logs -f --tail=100 -- mypod\n", ""},
		{"S24 dry-run after path is not global", []string{"k8s", "logs", "mypod", "--dry-run"}, 1, "", "unknown flag --dry-run"},
		{"S24 command flag dry_run is its own", []string{"--dry-run", "dev", "deploy", "run", "prod", "--dry-run"}, 0, "deploy.sh --dry-run prod\n", ""},
		{"S27 dash source after literal --", []string{"--dry-run", "files", "copy", "--", "-rf", "dst"}, 0, "cp -- -rf dst\n", ""},
		{"S27 force", []string{"--dry-run", "files", "copy", "--force", "a", "b"}, 0, "cp -f -- a b\n", ""},
		{"S28 dash value after --", []string{"--dry-run", "k8s", "logs", "--", "-h"}, 0, "kubectl logs --tail=100 -- -h\n", ""},
		{"namespace and module path", []string{"--dry-run", "dev", "deploy", "run", "prod"}, 0, "deploy.sh prod\n", ""},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, code := runFutiBinary(t, mods, c.args...)
			if code != c.wantCode || stdout != c.wantStdout || !strings.Contains(stderr, c.wantStderr) {
				t.Fatalf("futi %v:\ncode=%d stdout=%q stderr=%q\nwant code=%d stdout=%q stderr containing %q",
					c.args, code, stdout, stderr, c.wantCode, c.wantStdout, c.wantStderr)
			}
		})
	}
}

// scenarioDirWith returns a modules directory holding the given files
// (relative path -> content).
func scenarioDirWith(t *testing.T, files map[string]string) string {
	t.Helper()
	mods := t.TempDir()
	for name, content := range files {
		path := filepath.Join(mods, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeMainTestFile(t, path, content)
	}
	return mods
}

// S16: a word in a module with only a default cmd is a value; once a command
// of that name appears, it becomes a subcommand, and the value moves after --.
func TestScenarioS16SubcommandShadowsValue(t *testing.T) {
	const echoWithBuild = `
name: echo
args:
  - {name: words, variadic: true}
cmd: "echo {{.words}}"
commands:
  build:
    cmd: "echo built"
`
	mods := scenarioDirWith(t, map[string]string{"echo.yaml": echoWithBuild})
	tests := []struct {
		name       string
		args       []string
		wantStdout string
	}{
		{"word is a subcommand now", []string{"--dry-run", "echo", "build"}, "echo built\n"},
		{"value after --", []string{"--dry-run", "echo", "--", "build"}, "echo build\n"},
		{"not the first word", []string{"--dry-run", "echo", "say", "build"}, "echo say build\n"},
		{"validate warns", []string{"modules", "validate", "echo"}, "WARNING: command \"(default)\": words build are subcommands; pass such a value after --\n"},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, code := runFutiBinary(t, mods, c.args...)
			if code != 0 || stdout != c.wantStdout {
				t.Fatalf("code=%d stdout=%q stderr=%q, want 0 and %q", code, stdout, stderr, c.wantStdout)
			}
		})
	}
}

// S17: a user command named help, versus the system help.
func TestScenarioS17SystemHelp(t *testing.T) {
	mods := scenarioModules(t)
	for _, args := range [][]string{{"help", "git"}, {"git", "-h"}} {
		stdout, stderr, code := runFutiBinary(t, mods, args...)
		if code != 0 || !strings.HasPrefix(stdout, "usage: futi git\n") || !strings.Contains(stdout, "subcommands:") {
			t.Fatalf("futi %v: code=%d stdout=%q stderr=%q, want code 0 and the help of git", args, code, stdout, stderr)
		}
	}
}

// S18: a path addressed by two entities is an error everywhere it is read.
func TestScenarioS18PathCollision(t *testing.T) {
	mods := t.TempDir()
	copyScenarioDir(t, filepath.Join("testdata", "scenarios", "collision"), mods)
	const want = `path "dev deploy" is ambiguous: command "deploy" of module dev`
	for _, args := range [][]string{
		{"--dry-run", "dev", "deploy", "run", "prod"},
		{"--dry-run", "dev", "deploy"},
		{"modules", "validate", "dev"},
	} {
		// validate reports errors on stdout, the other commands on stderr.
		stdout, stderr, code := runFutiBinary(t, mods, args...)
		if code != 1 || !strings.Contains(stdout+stderr, want) {
			t.Fatalf("futi %v: code=%d stdout=%q stderr=%q, want 1 and %q", args, code, stdout, stderr, want)
		}
	}
}

// S19: a value equal to a subcommand name is passed after --; the warning of
// §4.3 applies to a string positional only.
func TestScenarioS19ValueEqualToSubcommand(t *testing.T) {
	mods := scenarioDirWith(t, map[string]string{"dbs.yaml": `
commands:
  migrate:
    args:
      - {name: label}
    cmd: "db-migrate {{if .label}}--label={{.label}}{{end}}"
    commands:
      up:
        cmd: "db-migrate up"
`})
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--dry-run", "dbs", "migrate", "up"}, "db-migrate up\n"},
		{[]string{"--dry-run", "dbs", "migrate", "--", "up"}, "db-migrate --label=up\n"},
		{[]string{"--dry-run", "dbs", "migrate", "x"}, "db-migrate --label=x\n"},
	}
	for _, c := range tests {
		stdout, stderr, code := runFutiBinary(t, mods, c.args...)
		if code != 0 || stdout != c.want {
			t.Fatalf("futi %v: code=%d stdout=%q stderr=%q, want 0 and %q", c.args, code, stdout, stderr, c.want)
		}
	}
	// An int positional cannot hold the name, so -- up is a plain int error.
	_, stderr, code := runFutiBinary(t, scenarioModules(t), "--dry-run", "db", "migrate", "--", "up")
	if code != 1 || !strings.Contains(stderr, `invalid int value "up" for <steps>`) {
		t.Fatalf("code=%d stderr=%q, want 1 and invalid int value", code, stderr)
	}
}

// S25: completion after the path offers flags for a dash and nothing for a
// positional value.
func TestScenarioS25Completion(t *testing.T) {
	mods := scenarioModules(t)
	tests := []struct {
		name string
		args []string
		want []string // stdout lines
	}{
		{"positional value", []string{"__complete", "k8s", "logs", ""}, []string{":4"}},
		{"flags after a dash", []string{"__complete", "k8s", "logs", "-"}, []string{"--follow", "--help\tShow help", "--tail\t(default: 100)", "-f", ":4"}},
		{"after a positional of copy", []string{"__complete", "files", "copy", "a.txt", ""}, []string{":4"}},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, code := runFutiBinary(t, mods, c.args...)
			got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if code != 0 || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q, want lines %q", code, stdout, stderr, c.want)
			}
		})
	}
}

// S26: an MCP-style call binds JSON inputs to the template; the schema
// guards the pod name.
func TestScenarioS26JSONInputs(t *testing.T) {
	logsArgs := []inputs.Spec{{Name: "pod", Required: true}}
	logsFlags := []inputs.Spec{
		{Name: "follow", Type: "bool", Short: "f", Flag: true},
		{Name: "tail", Type: "int", Default: "100", HasDefault: true, Flag: true},
	}
	runArgs := []inputs.Spec{{Name: "image", Required: true}, {Name: "docker_args", Rest: true}}

	tests := []struct {
		name        string
		args, flags []inputs.Spec
		cmd         string
		given       map[string]any
		want        []string
	}{
		{"k8s logs", logsArgs, logsFlags,
			"kubectl logs {{if .follow}}-f{{end}} --tail={{.tail}} -- {{.pod}}",
			map[string]any{"pod": "mypod", "follow": true},
			[]string{"kubectl", "logs", "-f", "--tail=100", "--", "mypod"}},
		{"docker run with arbitrary arguments", runArgs, nil,
			"docker run {{.image}} {{.docker_args}}",
			map[string]any{"image": "nginx", "docker_args": []any{"-it", "--rm"}},
			[]string{"docker", "run", "nginx", "-it", "--rm"}},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			tpl, err := argvtmpl.Parse(c.cmd)
			if err != nil {
				t.Fatal(err)
			}
			ti := inputs.TemplateInputs(c.args, c.flags)
			values, err := inputs.Bind(c.args, c.flags, c.given)
			if err != nil {
				t.Fatal(err)
			}
			argv, err := tpl.Eval(ti, argvtmpl.Values{Inputs: values})
			if err != nil {
				t.Fatal(err)
			}
			if got := argvtmpl.Argv(argv); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("argv = %q, want %q", got, c.want)
			}
		})
	}

	// The k8s pod property carries the dash guard of the schema.
	tpl, err := argvtmpl.Parse("kubectl logs {{.pod}} {{if .follow}}-f{{end}} --tail={{.tail}}")
	if err != nil {
		t.Fatal(err)
	}
	ti := inputs.TemplateInputs(logsArgs, logsFlags)
	schema := inputs.Schema(logsArgs, logsFlags, func(name string) bool { return tpl.Guarded(ti, name) })
	props, _ := schema["properties"].(map[string]any)
	pod, _ := props["pod"].(map[string]any)
	if !reflect.DeepEqual(pod["not"], map[string]any{"pattern": "^-"}) || pod["type"] != "string" {
		t.Fatalf("schema of pod = %v, want a string with not pattern ^-", pod)
	}
}

// S28: -h is help unless it is a value or follows --; the name help is
// reserved for inputs.
func TestScenarioS28Help(t *testing.T) {
	mods := scenarioModules(t)
	stdout, stderr, code := runFutiBinary(t, mods, "k8s", "logs", "mypod", "-h")
	if code != 0 || !strings.Contains(stdout, "usage: futi k8s logs [flags] <pod>") {
		t.Fatalf("code=%d stdout=%q stderr=%q, want help of k8s logs", code, stdout, stderr)
	}
	reserved := scenarioDirWith(t, map[string]string{"h.yaml": `
commands:
  x:
    args:
      - {name: help}
    cmd: "echo {{.help}}"
`})
	stdout, stderr, code = runFutiBinary(t, reserved, "modules", "validate", "h")
	if code != 1 || !strings.Contains(stdout, `arg "help": name "help" is reserved`) {
		t.Fatalf("code=%d stdout=%q stderr=%q, want 1 and the reserved name error", code, stdout, stderr)
	}
}
