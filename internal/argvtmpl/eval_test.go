package argvtmpl

import (
	"reflect"
	"testing"
)

func scalar(s string) Value       { return Value{Scalar: s} }
func list(s ...string) Value      { return Value{List: s} }
func noEnv(string) (string, bool) { return "", false }

// mustParse parses the string form or fails the test.
func mustParse(t *testing.T, src string) *Template {
	t.Helper()
	tmpl, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return tmpl
}

func TestEval(t *testing.T) {
	var (
		pod     = Input{Name: "pod"}
		follow  = Input{Name: "follow", Kind: Bool, Flag: true}
		tail    = Input{Name: "tail", Kind: Int, Flag: true}
		optTail = Input{Name: "tail", Kind: Int, Flag: true, Optional: true}
		flagF   = Input{Name: "f", Kind: Bool, Flag: true}
		x       = Input{Name: "x"}
		a       = Input{Name: "a"}
		paths   = Input{Name: "paths", Variadic: true, Optional: true}
		envs    = Input{Name: "envs", Flag: true, Variadic: true, Optional: true}
		k8s     = `kubectl logs {{if .follow}}-f{{end}} --tail={{.tail}} -- {{.pod}}`
		env     = func(name string) (string, bool) {
			switch name {
			case "HOME":
				return "/home/u", true
			case "EMPTY":
				return "", true
			}
			return "", false
		}
	)
	tests := []struct {
		name    string
		src     string
		items   []string // list form when non-nil
		inputs  []Input
		values  map[string]Value
		want    []string
		wantErr string
	}{
		{"S1", k8s, nil, []Input{pod, follow, tail},
			map[string]Value{"pod": scalar("mypod"), "follow": scalar("false"), "tail": scalar("100")},
			[]string{"kubectl", "logs", "--tail=100", "--", "mypod"}, ""},
		{"S2", k8s, nil, []Input{pod, follow, tail},
			map[string]Value{"pod": scalar("mypod"), "follow": scalar("true"), "tail": scalar("50")},
			[]string{"kubectl", "logs", "-f", "--tail=50", "--", "mypod"}, ""},
		{"S3", k8s, nil, []Input{pod, follow, tail},
			map[string]Value{"pod": scalar("mypod"), "follow": scalar("true"), "tail": scalar("100")},
			[]string{"kubectl", "logs", "-f", "--tail=100", "--", "mypod"}, ""},
		{"unset input drops the element", `logs --tail={{.tail}} x`, nil, []Input{optTail}, nil,
			[]string{"logs", "x"}, ""},
		{"empty value keeps an empty element", `a {{.x}} b`, nil, []Input{x},
			map[string]Value{"x": scalar("")}, []string{"a", "", "b"}, ""},
		{"literal double quotes are an empty element", `a "" b`, nil, nil, nil, []string{"a", "", "b"}, ""},
		{"literal single quotes are an empty element", `a '' b`, nil, nil, nil, []string{"a", "", "b"}, ""},
		{"empty only because of if is dropped", `a {{if .f}}{{end}} b`, nil, []Input{flagF},
			map[string]Value{"f": scalar("true")}, []string{"a", "b"}, ""},
		{"false if is dropped", `a {{if .f}}-f{{end}} b`, nil, []Input{flagF},
			map[string]Value{"f": scalar("false")}, []string{"a", "b"}, ""},
		{"variadic with no values", `ls {{.paths}}`, nil, []Input{paths}, nil, []string{"ls"}, ""},
		{"variadic with two values", `ls {{.paths}} end`, nil, []Input{paths},
			map[string]Value{"paths": list("a b", "c")}, []string{"ls", "a b", "c", "end"}, ""},
		{"range", `run {{range .envs}}-e {{.}} {{end}}img`, nil, []Input{envs},
			map[string]Value{"envs": list("A", "B")}, []string{"run", "-e", "A", "-e", "B", "img"}, ""},
		{"range glued", `run {{range .envs}}--env={{.}} {{end}}`, nil, []Input{envs},
			map[string]Value{"envs": list("A=1", "B=2")}, []string{"run", "--env=A=1", "--env=B=2"}, ""},
		{"list form keeps spaces", "", []string{"echo", "a b", "{{.x}} y"}, []Input{x},
			map[string]Value{"x": scalar("1 2")}, []string{"echo", "a b", "1 2 y"}, ""},
		{"list form range gives an element per value", "", []string{"run", "{{range .envs}}--env={{.}}{{end}}"}, []Input{envs},
			map[string]Value{"envs": list("A", "B")}, []string{"run", "--env=A", "--env=B"}, ""},
		{"list form whole variadic", "", []string{"ls", "{{.paths}}"}, []Input{paths},
			map[string]Value{"paths": list("a", "b")}, []string{"ls", "a", "b"}, ""},
		{"list form empty item", "", []string{"a", "", "b"}, nil, nil, []string{"a", "", "b"}, ""},
		{"list form false if", "", []string{"a", "{{if .f}}-f{{end}}"}, []Input{flagF},
			map[string]Value{"f": scalar("false")}, []string{"a"}, ""},
		{"value in quotes is one element", `"{{.a}} x"`, nil, []Input{a},
			map[string]Value{"a": scalar("1 2")}, []string{"1 2 x"}, ""},
		{"value with quotes is data", `echo {{.a}}`, nil, []Input{a},
			map[string]Value{"a": scalar(`"q" 'r'`)}, []string{"echo", `"q" 'r'`}, ""},
		{"escapes in double quotes", `echo "a\"b\\c" 'd\e'`, nil, nil, nil, []string{"echo", `a"b\c`, `d\e`}, ""},
		{"env and module", `{{.module_root}}/bin {{.module_name}} {{.env.HOME}}`, nil, nil, nil,
			[]string{"/root/m/bin", "mod", "/home/u"}, ""},
		{"env truth", `a {{if .env.HOME}}h{{end}} {{if .env.EMPTY}}e{{end}} {{if .env.NOPE}}n{{end}}`, nil, nil, nil,
			[]string{"a", "h"}, ""},
		{"unset env outside condition", `echo {{.env.NOPE}}`, nil, nil, nil, nil, "environment variable NOPE is not set"},
		{"empty argv", `{{if .f}}x{{end}}`, nil, []Input{flagF},
			map[string]Value{"f": scalar("false")}, nil, "command rendered to empty argv"},
		{"else if", `{{if eq .x "a"}}A{{else if eq .x "b"}}B{{else}}C{{end}}`, nil, []Input{x},
			map[string]Value{"x": scalar("b")}, []string{"B"}, ""},
		{"else", `{{if eq .x "a"}}A{{else if eq .x "b"}}B{{else}}C{{end}}`, nil, []Input{x},
			map[string]Value{"x": scalar("z")}, []string{"C"}, ""},
		{"set scalar is true even when empty", `{{if .x}}set{{end}}`, nil, []Input{x},
			map[string]Value{"x": scalar("")}, []string{"set"}, ""},
		{"zero int is set", `{{if .tail}}--tail={{.tail}}{{end}}`, nil, []Input{optTail},
			map[string]Value{"tail": scalar("0")}, []string{"--tail=0"}, ""},
		{"variadic truth", `a {{if .paths}}p{{end}} {{if not .paths}}none{{end}}`, nil, []Input{paths},
			map[string]Value{"paths": list()}, []string{"a", "none"}, ""},
		{"and or not", `{{if and .f (or .x (not .f))}}y{{else}}n{{end}}`, nil, []Input{flagF, x},
			map[string]Value{"f": scalar("true")}, []string{"n"}, ""},
		{"eq on unset is false", `{{if eq .tail 0}}z{{else}}u{{end}}`, nil, []Input{optTail}, nil, []string{"u"}, ""},
		{"eq on int compares canonical form", `{{if eq .tail 007}}seven{{end}}`, nil, []Input{optTail},
			map[string]Value{"tail": scalar("7")}, []string{"seven"}, ""},
		{"eq literal with leading zero is decimal", `{{if eq .tail 010}}ten{{else}}other{{end}}`, nil, []Input{optTail},
			map[string]Value{"tail": scalar("10")}, []string{"ten"}, ""},
		{"eq literal with leading zero is not octal", `{{if eq .tail 010}}eight{{else}}other{{end}}`, nil, []Input{optTail},
			map[string]Value{"tail": scalar("8")}, []string{"other"}, ""},
		{"eq on bool", `{{if eq .f true}}t{{end}}`, nil, []Input{flagF},
			map[string]Value{"f": scalar("true")}, []string{"t"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var tmpl *Template
			if tc.items != nil {
				var err error
				if tmpl, err = ParseList(tc.items); err != nil {
					t.Fatalf("ParseList(%q) error: %v", tc.items, err)
				}
			} else {
				tmpl = mustParse(t, tc.src)
			}
			args, err := tmpl.Eval(tc.inputs, Values{Inputs: tc.values, Env: env, ModuleName: "mod", ModuleRoot: "/root/m"})
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("Eval error = %v, want %q (argv %q)", err, tc.wantErr, Argv(args))
				}
				return
			}
			if err != nil {
				t.Fatalf("Eval error: %v", err)
			}
			if got := Argv(args); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEvalDefaultEnvIsProcessEnv(t *testing.T) {
	t.Setenv("FUTI_ARGVTMPL_TEST", "v")
	args, err := mustParse(t, `echo {{.env.FUTI_ARGVTMPL_TEST}}`).Eval(nil, Values{})
	if err != nil {
		t.Fatalf("Eval error: %v", err)
	}
	if got, want := Argv(args), []string{"echo", "v"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Eval = %q, want %q", got, want)
	}
}

func TestEvalOrigin(t *testing.T) {
	var (
		user = Input{Name: "user"}
		host = Input{Name: "host"}
		x    = Input{Name: "x"}
		src  = Input{Name: "src", Variadic: true}
	)
	tests := []struct {
		name   string
		src    string
		inputs []Input
		values map[string]Value
		want   []Arg
	}{
		{"glued inputs", `ssh {{.user}}@{{.host}}`, []Input{user, host},
			map[string]Value{"user": scalar("u"), "host": scalar("h")},
			[]Arg{{Value: "ssh"}, {Value: "u@h", First: "user", Users: []string{"user", "host"}, firstValue: "u"}}},
		{"empty first value", `ssh {{.user}}@{{.host}}`, []Input{user, host},
			map[string]Value{"user": scalar(""), "host": scalar("h")},
			[]Arg{{Value: "ssh"}, {Value: "@h", Users: []string{"host"}}}},
		{"literal first", `run --opt={{.x}}`, []Input{x},
			map[string]Value{"x": scalar("-v")},
			[]Arg{{Value: "run"}, {Value: "--opt=-v", Users: []string{"x"}}}},
		{"env first", `run {{.env.HOME}}{{.x}}`, []Input{x},
			map[string]Value{"x": scalar("v")},
			[]Arg{{Value: "run"}, {Value: "/hv", Users: []string{"x"}}}},
		{"after an exempting --", `cp -- {{.src}}`, []Input{src},
			map[string]Value{"src": list("a", "b")},
			[]Arg{{Value: "cp"}, {Value: "--"}, {Value: "a", First: "src", Users: []string{"src"}, Exempt: true, firstValue: "a"},
				{Value: "b", First: "src", Users: []string{"src"}, Exempt: true, firstValue: "b"}}},
		{"after a -- that does not exempt", `kubectl exec -- ls {{.x}}`, []Input{x},
			map[string]Value{"x": scalar("v")},
			[]Arg{{Value: "kubectl"}, {Value: "exec"}, {Value: "--"}, {Value: "ls"}, {Value: "v", First: "x", Users: []string{"x"}, firstValue: "v"}}},
	}
	env := func(string) (string, bool) { return "/h", true }
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mustParse(t, tc.src).Eval(tc.inputs, Values{Inputs: tc.values, Env: env})
			if err != nil {
				t.Fatalf("Eval error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval =\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}

// Templates that Check rejects are not required to evaluate meaningfully,
// but Eval must not panic on them.
func TestEvalRejectedTemplatesDoNotPanic(t *testing.T) {
	inputs := []Input{{Name: "v", Variadic: true}, {Name: "s"}, {Name: "b", Kind: Bool}}
	values := Values{Inputs: map[string]Value{"v": list("1", "2"), "s": scalar("x"), "b": scalar("true")}, Env: noEnv}
	for _, src := range []string{
		`a --x={{.v}}`, `"{{.v}}"`, `{{range .s}}{{.}}{{end}} z`, `{{.b}}`, `{{.unknown}} z`,
		`{{if eq .v "1"}}a{{end}} z`, `{{range .unknown}}{{.}}{{end}} z`,
	} {
		if _, err := mustParse(t, src).Eval(inputs, values); err != nil && err.Error() == "" {
			t.Errorf("Eval(%q): empty error", src)
		}
	}
}
