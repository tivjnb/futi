package argvtmpl

import (
	"errors"
	"testing"
)

func TestCheckDash(t *testing.T) {
	var (
		pod     = Input{Name: "pod"}
		path    = Input{Name: "path"}
		user    = Input{Name: "user"}
		host    = Input{Name: "host"}
		n       = Input{Name: "n", Kind: Int}
		nAllow  = Input{Name: "n", Kind: Int, AllowDash: true}
		tail    = Input{Name: "tail", Kind: Int, Flag: true}
		pattern = Input{Name: "pattern"}
		paths   = Input{Name: "paths", Variadic: true, Optional: true}
		force   = Input{Name: "force", Kind: Bool, Flag: true}
		src     = Input{Name: "src", Variadic: true}
		dst     = Input{Name: "dst"}
		image   = Input{Name: "image"}
		rest    = Input{Name: "docker_args", Rest: true}
		rev     = Input{Name: "rev"}
		ignore  = Input{Name: "ignore_case", Flag: true}
		envs    = Input{Name: "envs", Flag: true, Variadic: true}
	)
	tests := []struct {
		name    string
		src     string
		inputs  []Input
		values  map[string]Value
		program string
		want    string // "" for no error
	}{
		{"X5 literal after --", `kubectl exec {{.pod}} -- ls {{.path}}`, []Input{pod, path},
			map[string]Value{"pod": scalar("p"), "path": scalar("-la")}, "kubectl",
			`value "-la" for <path> starts with "-" and would be read as an option by kubectl; use --opt={{.path}}, put a literal -- before it, or set allow_dash`},
		{"X6 glued value first", `ssh {{.user}}@{{.host}}`, []Input{user, host},
			map[string]Value{"user": scalar("-oProxyCommand=x"), "host": scalar("h")}, "ssh",
			`value "-oProxyCommand=x" for <user> starts with "-" and would be read as an option by ssh; use --opt={{.user}}, put a literal -- before it, or set allow_dash`},
		{"X6 glued value not first", `ssh {{.user}}@{{.host}}`, []Input{user, host},
			map[string]Value{"user": scalar("u"), "host": scalar("-h")}, "ssh", ""},
		{"X7 negative int", `tail {{.n}} f`, []Input{n},
			map[string]Value{"n": scalar("-5")}, "tail",
			`value "-5" for <n> starts with "-" and would be read as an option by tail; use --opt={{.n}}, put a literal -- before it, or set allow_dash`},
		{"S8 after exempting --", `kubectl logs --tail={{.tail}} -- {{.pod}}`, []Input{tail, pod},
			map[string]Value{"tail": scalar("100"), "pod": scalar("--weird-pod-name")}, "kubectl", ""},
		{"S9 value after literal", `kubectl logs --tail={{.tail}} -- {{.pod}}`, []Input{tail, pod},
			map[string]Value{"tail": scalar("-5"), "pod": scalar("p")}, "kubectl", ""},
		{"S12 grep after --", `grep -- {{.pattern}} {{.paths}}`, []Input{pattern, paths},
			map[string]Value{"pattern": scalar("-v"), "paths": list("src")}, "grep", ""},
		{"S27 cp after --", `cp {{if .force}}-f{{end}} -- {{.src}} {{.dst}}`, []Input{force, src, dst},
			map[string]Value{"force": scalar("true"), "src": list("-rf"), "dst": scalar("d")}, "cp", ""},
		{"allow_dash", `tail {{.n}} f`, []Input{nAllow}, map[string]Value{"n": scalar("-5")}, "tail", ""},
		{"rest is not checked", `docker run {{.image}} {{.docker_args}}`, []Input{image, rest},
			map[string]Value{"image": scalar("nginx"), "docker_args": list("-it", "--rm")}, "docker", ""},
		{"git hint", `git log {{.rev}}`, []Input{rev}, map[string]Value{"rev": scalar("-p")}, "git",
			`value "-p" for <rev> starts with "-" and would be read as an option by git; use --opt={{.rev}}, put a literal -- before it, or set allow_dash, or --end-of-options`},
		{"flag shown as --name", `grep {{.ignore_case}}`, []Input{ignore}, map[string]Value{"ignore_case": scalar("-x")}, "grep",
			`value "-x" for --ignore-case starts with "-" and would be read as an option by grep; use --opt={{.ignore_case}}, put a literal -- before it, or set allow_dash`},
		{"range element", `run {{range .envs}}{{.}} {{end}}`, []Input{envs}, map[string]Value{"envs": list("a", "-b")}, "run",
			`value "-b" for --envs starts with "-" and would be read as an option by run; use --opt={{.envs}}, put a literal -- before it, or set allow_dash`},
		{"value without dash", `tail {{.n}} f`, []Input{n}, map[string]Value{"n": scalar("5")}, "tail", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args, err := mustParse(t, tc.src).Eval(tc.inputs, Values{Inputs: tc.values, Env: noEnv})
			if err != nil {
				t.Fatalf("Eval error: %v", err)
			}
			err = CheckDash(args, tc.inputs, tc.program)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckDash error = %v, want nil", err)
				}
				return
			}
			var de *DashError
			if !errors.As(err, &de) {
				t.Fatalf("CheckDash error = %v, want *DashError", err)
			}
			if err.Error() != tc.want {
				t.Errorf("CheckDash error =\n%s\nwant\n%s", err, tc.want)
			}
		})
	}
}

func TestCheckBatch(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		args []Arg
		want string // "" for no error
	}{
		{"value with & for .CMD", `/opt/bin/deploy.CMD`,
			[]Arg{{Value: "deploy.CMD"}, {Value: "a&b", First: "x", Users: []string{"x"}}},
			`value for <x> contains "&", unsafe for batch file deploy.CMD`},
		{".bat", `deploy.bat`, []Arg{{Value: "a%b", Users: []string{"x", "y"}}},
			`value for <x> contains "%", unsafe for batch file deploy.bat`},
		{"newline is escaped", `deploy.cmd`, []Arg{{Value: "a\nb", Users: []string{"x"}}},
			`value for <x> contains "\n", unsafe for batch file deploy.cmd`},
		{"carriage return is escaped", `deploy.cmd`, []Arg{{Value: "a\rb", Users: []string{"x"}}},
			`value for <x> contains "\r", unsafe for batch file deploy.cmd`},
		{"double quote", `deploy.cmd`, []Arg{{Value: `a"b`, Users: []string{"x"}}},
			`value for <x> contains "\"", unsafe for batch file deploy.cmd`},
		{"first unsafe character", `deploy.cmd`, []Arg{{Value: "a|b>c", Users: []string{"x"}}},
			`value for <x> contains "|", unsafe for batch file deploy.cmd`},
		{"trailing dot is dropped by Windows", `deploy.cmd.`, []Arg{{Value: "a&b", Users: []string{"x"}}},
			`value for <x> contains "&", unsafe for batch file deploy.cmd.`},
		{"trailing space is dropped by Windows", `deploy.bat `, []Arg{{Value: "a&b", Users: []string{"x"}}},
			`value for <x> contains "&", unsafe for batch file deploy.bat `},
		{"trailing dots and spaces", `deploy.cmd. . `, []Arg{{Value: "a&b", Users: []string{"x"}}},
			`value for <x> contains "&", unsafe for batch file deploy.cmd. . `},
		{"exe with trailing dot is not checked", `deploy.exe.`, []Arg{{Value: "a&b", Users: []string{"x"}}}, ""},
		{"exe is not checked", `/opt/bin/deploy.exe`, []Arg{{Value: "a&b", Users: []string{"x"}}}, ""},
		{"literal without values", `deploy.cmd`, []Arg{{Value: "a&b"}}, ""},
		{"safe value", `deploy.cmd`, []Arg{{Value: "a-b c", Users: []string{"x"}}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckBatch(tc.exe, tc.args)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckBatch error = %v, want nil", err)
				}
				return
			}
			var be *BatchError
			if !errors.As(err, &be) {
				t.Fatalf("CheckBatch error = %v, want *BatchError", err)
			}
			if err.Error() != tc.want {
				t.Errorf("CheckBatch error = %s, want %s", err, tc.want)
			}
		})
	}
}

func TestCheckBatchRestValue(t *testing.T) {
	inputs := []Input{{Name: "docker_args", Rest: true}}
	args, err := mustParse(t, `deploy {{.docker_args}}`).Eval(inputs,
		Values{Inputs: map[string]Value{"docker_args": list("ok", "a&b")}, Env: noEnv})
	if err != nil {
		t.Fatalf("Eval error: %v", err)
	}
	want := `value for <docker_args> contains "&", unsafe for batch file deploy.cmd`
	if err := CheckBatch(`C:/bin/deploy.cmd`, args); err == nil || err.Error() != want {
		t.Errorf("CheckBatch error = %v, want %s", err, want)
	}
}
