package inputs

import (
	"reflect"
	"strings"
	"testing"
)

type node struct {
	label       string
	args, flags []Spec
}

var (
	k8sLogs = node{
		label: "futi k8s logs",
		args:  []Spec{{Name: "pod", Required: true}},
		flags: []Spec{{Name: "follow", Type: "bool", Short: "f", Flag: true}, {Name: "tail", Type: "int", Default: "100", HasDefault: true, Flag: true}},
	}
	filesCopy = node{
		label: "futi files copy",
		args:  []Spec{{Name: "src", Required: true, Variadic: true}, {Name: "dst", Required: true}},
		flags: []Spec{{Name: "force", Type: "bool", Flag: true}},
	}
	filesGrep = node{
		label: "futi files grep",
		args:  []Spec{{Name: "pattern", Required: true}, {Name: "paths", Variadic: true}},
		flags: []Spec{{Name: "ignore_case", Type: "bool", Short: "i", Flag: true}, {Name: "follow", Type: "bool", Short: "f", Flag: true}},
	}
	dockerBuild = node{
		label: "futi docker build",
		args:  []Spec{{Name: "context", Default: ".", HasDefault: true}},
		flags: []Spec{{Name: "tag", Required: true, Short: "t", Flag: true}, {Name: "file", Default: "Dockerfile", HasDefault: true, Flag: true}},
	}
	dockerRun = node{
		label: "futi docker run",
		args:  []Spec{{Name: "image", Required: true}, {Name: "extra", Rest: true}},
		flags: []Spec{{Name: "rm", Type: "bool", Flag: true}},
	}
	gitCommit = node{
		label: "futi git commit",
		flags: []Spec{{Name: "message", Short: "m", Flag: true}, {Name: "amend", Type: "bool", Flag: true}},
	}
	dockerPs = node{label: "futi docker"}
	shortcut = node{
		label: "futi k",
		args:  []Spec{{Name: "kargs", Rest: true}},
		flags: []Spec{{Name: "verbose", Type: "bool", Short: "v", Flag: true}},
	}
	labels = node{
		label: "futi img label",
		args:  []Spec{{Name: "first", Required: true}, {Name: "files", Required: true, Variadic: true}, {Name: "last", Required: true}},
		flags: []Spec{{Name: "label", Short: "l", Variadic: true, Flag: true}},
	}
	optionalArgs = node{
		label: "futi x y",
		args:  []Spec{{Name: "a", Required: true}, {Name: "b_c"}},
		flags: []Spec{{Name: "dry", Type: "bool", Flag: true}},
	}
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		node node
		toks []string
		want map[string]any
		help bool
		err  string // exact error message; "" means no error
	}{
		// Positionals and flags in any order (S1-S4).
		{name: "S1 positional", node: k8sLogs, toks: []string{"mypod"}, want: map[string]any{"pod": "mypod"}},
		{name: "S2 flags after", node: k8sLogs, toks: []string{"mypod", "--follow", "--tail", "50"}, want: map[string]any{"pod": "mypod", "follow": "true", "tail": "50"}},
		{name: "S3 bool does not eat positional", node: k8sLogs, toks: []string{"--follow", "mypod"}, want: map[string]any{"pod": "mypod", "follow": "true"}},
		{name: "S4 short before", node: k8sLogs, toks: []string{"-f", "mypod"}, want: map[string]any{"pod": "mypod", "follow": "true"}},
		{name: "no tokens", node: k8sLogs, toks: nil, want: map[string]any{}},

		// 1: "--".
		{name: "S8 dash value after --", node: k8sLogs, toks: []string{"--", "--weird-pod-name"}, want: map[string]any{"pod": "--weird-pod-name"}},
		{name: "-h after -- is a value", node: k8sLogs, toks: []string{"--", "-h"}, want: map[string]any{"pod": "-h"}},
		{name: "S12 grep -- -v src", node: filesGrep, toks: []string{"--", "-v", "src"}, want: map[string]any{"pattern": "-v", "paths": []string{"src"}}},
		{name: "second -- is a value", node: filesGrep, toks: []string{"--", "x", "--"}, want: map[string]any{"pattern": "x", "paths": []string{"--"}}},

		// 2: --name=value.
		{name: "S5 --follow=false", node: k8sLogs, toks: []string{"mypod", "--follow=false"}, want: map[string]any{"pod": "mypod", "follow": "false"}},
		{name: "bool =ON normalized", node: k8sLogs, toks: []string{"mypod", "--follow=ON"}, want: map[string]any{"pod": "mypod", "follow": "true"}},
		{name: "bool =0", node: k8sLogs, toks: []string{"mypod", "--follow=0"}, want: map[string]any{"pod": "mypod", "follow": "false"}},
		{name: "S20 --message=", node: gitCommit, toks: []string{"--message=fix: x"}, want: map[string]any{"message": "fix: x"}},
		{name: "empty value", node: gitCommit, toks: []string{"--message="}, want: map[string]any{"message": ""}},
		{name: "underscore name as dash", node: filesGrep, toks: []string{"--ignore-case", "x"}, want: map[string]any{"pattern": "x", "ignore_case": "true"}},
		{name: "invalid bool value", node: k8sLogs, toks: []string{"mypod", "--follow=x"}, err: `invalid bool value "x" for --follow (valid: true, false, 1, 0, yes, no, on, off)`},

		// 3: --name / -s.
		{name: "S5 --follow false", node: k8sLogs, toks: []string{"mypod", "--follow", "false"}, err: "bool flag --follow takes no separate value; use --follow=false"},
		{name: "-f TRUE", node: k8sLogs, toks: []string{"mypod", "-f", "TRUE"}, err: "bool flag --follow takes no separate value; use --follow=TRUE"},
		{name: "S9 --tail -5", node: k8sLogs, toks: []string{"mypod", "--tail", "-5"}, want: map[string]any{"pod": "mypod", "tail": "-5"}},
		{name: "-m -h is a value", node: gitCommit, toks: []string{"-m", "-h"}, want: map[string]any{"message": "-h"}},
		{name: "-m -x is a value", node: gitCommit, toks: []string{"-m", "-x"}, want: map[string]any{"message": "-x"}},
		{name: "-m --help is a value", node: gitCommit, toks: []string{"-m", "--help"}, want: map[string]any{"message": "--help"}},
		{name: "S20 -m value --amend", node: gitCommit, toks: []string{"-m", "fix: x", "--amend"}, want: map[string]any{"message": "fix: x", "amend": "true"}},
		{name: "value is a known flag", node: gitCommit, toks: []string{"--message", "--amend"}, err: "flag --message requires a value (got --amend; use --message=--amend)"},
		{name: "value is a known short", node: dockerBuild, toks: []string{"--file", "-t", "x"}, err: "flag --file requires a value (got -t; use --file=-t)"},
		{name: "value looks like short with =", node: dockerBuild, toks: []string{"--file", "-t=x"}, want: map[string]any{"file": "-t=x"}},
		{name: "value is a known long with =", node: gitCommit, toks: []string{"--message", "--amend=x"}, err: "flag --message requires a value (got --amend=x; use --message=--amend=x)"},
		{name: "value is --", node: gitCommit, toks: []string{"-m", "--"}, err: "flag --message requires a value (got --; use --message=--)"},
		{name: "S21 no value", node: gitCommit, toks: []string{"-m"}, err: "flag --message requires a value"},

		// 4: help.
		{name: "S28 -h", node: k8sLogs, toks: []string{"mypod", "-h"}, help: true},
		{name: "S28 --help", node: k8sLogs, toks: []string{"--help"}, help: true},
		{name: "help beats unknown flag", node: k8sLogs, toks: []string{"--bogus", "-h"}, help: true},
		{name: "help beats unexpected argument", node: k8sLogs, toks: []string{"a", "b", "--help"}, help: true},
		{name: "help beats missing value", node: gitCommit, toks: []string{"--message", "--amend", "-h"}, help: true},
		{name: "help stops scanning", node: gitCommit, toks: []string{"-h", "-m"}, help: true},
		{name: "help before rest start", node: dockerRun, toks: []string{"-h", "nginx"}, help: true},

		// 5: -<digits>.
		{name: "negative int positional", node: k8sLogs, toks: []string{"-5"}, want: map[string]any{"pod": "-5"}},
		{name: "negative decimal positional", node: k8sLogs, toks: []string{"-3.2"}, want: map[string]any{"pod": "-3.2"}},
		{name: "lone dash positional", node: k8sLogs, toks: []string{"-"}, want: map[string]any{"pod": "-"}},

		// 6: unknown flags.
		{name: "S23 unknown flag", node: k8sLogs, toks: []string{"mypod", "--unknown"}, err: `unknown flag --unknown for "futi k8s logs"`},
		{name: "unknown flag suggestion by distance", node: k8sLogs, toks: []string{"mypod", "--folow"}, err: `unknown flag --folow for "futi k8s logs" (did you mean --follow?)`},
		{name: "unknown flag suggestion by prefix", node: k8sLogs, toks: []string{"mypod", "--foll-mode"}, err: `unknown flag --foll-mode for "futi k8s logs" (did you mean --follow?)`},
		{name: "unknown flag hides value", node: k8sLogs, toks: []string{"mypod", "--x=secret"}, err: `unknown flag --x for "futi k8s logs"`},
		{name: "underscore spelling is unknown", node: filesGrep, toks: []string{"--ignore_case", "x"}, err: `unknown flag --ignore_case for "futi files grep" (did you mean --ignore-case?)`},
		{name: "§4.1 dry-run after path", node: k8sLogs, toks: []string{"x", "--dry-run"}, err: `unknown flag --dry-run for "futi k8s logs"`},
		{name: "short name: no noisy suggestion", node: dockerRun, toks: []string{"--it", "nginx"}, err: `unknown flag --it for "futi docker run"`},
		{name: "short name: distance 1 suggests", node: dockerRun, toks: []string{"--rn", "nginx"}, err: `unknown flag --rn for "futi docker run" (did you mean --rm?)`},
		{name: "empty long name", node: dockerRun, toks: []string{"--=x", "nginx"}, err: `unknown flag "--" for "futi docker run"`},
		{name: "empty short name", node: dockerRun, toks: []string{"-=x", "nginx"}, err: `unknown flag "-" for "futi docker run"`},
		{name: "unknown short", node: k8sLogs, toks: []string{"-x"}, err: `unknown flag -x for "futi k8s logs"`},
		{name: "combined shorts", node: filesGrep, toks: []string{"-fi", "x"}, err: `unknown flag -fi for "futi files grep" (short flags cannot be combined: use -f -i)`},
		{name: "combined with unknown letter", node: filesGrep, toks: []string{"-fx", "x"}, err: `unknown flag -fx for "futi files grep"`},
		{name: "short with = non-bool", node: dockerBuild, toks: []string{"-t=x"}, err: `short flag -t cannot take "=": use -t VALUE or --tag=VALUE`},
		{name: "short with = bool", node: k8sLogs, toks: []string{"-f=x"}, err: `short flag -f cannot take "=": use --follow=VALUE`},
		{name: "first error wins", node: k8sLogs, toks: []string{"--a", "--b"}, err: `unknown flag --a for "futi k8s logs"`},

		// 7: positional distribution.
		{name: "S10 copy a b c dir/", node: filesCopy, toks: []string{"a.txt", "b.txt", "c.txt", "dir/"}, want: map[string]any{"src": []string{"a.txt", "b.txt", "c.txt"}, "dst": "dir/"}},
		{name: "copy one positional fills tail", node: filesCopy, toks: []string{"dir/"}, want: map[string]any{"dst": "dir/"}},
		{name: "S11 grep -i TODO src lib", node: filesGrep, toks: []string{"-i", "TODO", "src", "lib"}, want: map[string]any{"pattern": "TODO", "paths": []string{"src", "lib"}, "ignore_case": "true"}},
		{name: "S11 grep TODO", node: filesGrep, toks: []string{"TODO"}, want: map[string]any{"pattern": "TODO"}},
		{name: "head variadic tail", node: labels, toks: []string{"a", "b", "c", "d", "e"}, want: map[string]any{"first": "a", "files": []string{"b", "c", "d"}, "last": "e"}},
		{name: "optional args in order", node: optionalArgs, toks: []string{"a", "b"}, want: map[string]any{"a": "a", "b_c": "b"}},
		{name: "S14 optional arg unset", node: dockerBuild, toks: []string{"--tag", "x"}, want: map[string]any{"tag": "x"}},

		// 8: rest.
		{name: "S15 run nginx -it --rm -p", node: dockerRun, toks: []string{"nginx", "-it", "--rm", "-p", "80:80"}, want: map[string]any{"image": "nginx", "extra": []string{"-it", "--rm", "-p", "80:80"}}},
		{name: "run -- nginx -- -it", node: dockerRun, toks: []string{"--", "nginx", "--", "-it"}, want: map[string]any{"image": "nginx", "extra": []string{"--", "-it"}}},
		{name: "run nginx -- -it", node: dockerRun, toks: []string{"nginx", "--", "-it"}, want: map[string]any{"image": "nginx", "extra": []string{"-it"}}},
		{name: "run nginx -- -- -it", node: dockerRun, toks: []string{"nginx", "--", "--", "-it"}, want: map[string]any{"image": "nginx", "extra": []string{"--", "-it"}}},
		{name: "run --rm nginx -it --rm", node: dockerRun, toks: []string{"--rm", "nginx", "-it", "--rm"}, want: map[string]any{"rm": "true", "image": "nginx", "extra": []string{"-it", "--rm"}}},
		{name: "run nginx -h is rest", node: dockerRun, toks: []string{"nginx", "-h"}, want: map[string]any{"image": "nginx", "extra": []string{"-h"}}},
		{name: "unknown flag before rest start", node: dockerRun, toks: []string{"-it", "nginx"}, err: `unknown flag -it for "futi docker run"`},
		{name: "rest empty", node: dockerRun, toks: []string{"nginx"}, want: map[string]any{"image": "nginx"}},
		{name: "rest only: own flag then words", node: shortcut, toks: []string{"-v", "get", "pods", "-v", "-h"}, want: map[string]any{"verbose": "true", "kargs": []string{"get", "pods", "-v", "-h"}}},
		{name: "rest only: negative number starts rest", node: shortcut, toks: []string{"-5", "-v"}, want: map[string]any{"kargs": []string{"-5", "-v"}}},
		{name: "rest only: after --", node: shortcut, toks: []string{"--", "-v", "--"}, want: map[string]any{"kargs": []string{"-v", "--"}}},
		{name: "rest only: unknown flag before start", node: shortcut, toks: []string{"--bogus", "get"}, err: `unknown flag --bogus for "futi k"`},

		// 9: repeats.
		{name: "S22 repeat: last wins", node: k8sLogs, toks: []string{"mypod", "--follow", "--follow=false"}, want: map[string]any{"pod": "mypod", "follow": "false"}},
		{name: "repeat string: last wins", node: gitCommit, toks: []string{"-m", "a", "--message", "b"}, want: map[string]any{"message": "b"}},
		{name: "variadic flag accumulates", node: labels, toks: []string{"-l", "a", "--label=b", "--label", "c", "x", "y", "z"}, want: map[string]any{"label": []string{"a", "b", "c"}, "first": "x", "files": []string{"y"}, "last": "z"}},

		// 10: unexpected argument.
		{name: "S7 extra", node: k8sLogs, toks: []string{"mypod", "extra"}, err: `unexpected argument "extra" (futi k8s logs takes 1 argument: <pod>)`},
		{name: "extra after bool flag", node: k8sLogs, toks: []string{"mypod", "--follow", "on"}, err: `unexpected argument "on" (futi k8s logs takes 1 argument: <pod>; did you mean --follow=on?)`},
		{name: "extra after short bool flag", node: k8sLogs, toks: []string{"mypod", "-f", "on"}, err: `unexpected argument "on" (futi k8s logs takes 1 argument: <pod>; did you mean --follow=on?)`},
		{name: "extra with optional arg", node: optionalArgs, toks: []string{"a", "b", "c"}, err: `unexpected argument "c" (futi x y takes 2 arguments: <a> [b-c])`},
		{name: "extra no arguments", node: dockerPs, toks: []string{"x"}, err: `unexpected argument "x" (futi docker takes no arguments)`},
		{name: "extra no arguments after bool", node: gitCommit, toks: []string{"--amend", "yes"}, err: `unexpected argument "yes" (futi git commit takes no arguments; did you mean --amend=yes?)`},
		{name: "extra after bool with = has no hint", node: optionalArgs, toks: []string{"a", "--dry=on", "b", "c"}, err: `unexpected argument "c" (futi x y takes 2 arguments: <a> [b-c])`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.node.args, tt.node.flags, tt.toks, tt.node.label)
			if tt.err != "" {
				if err == nil || err.Error() != tt.err {
					t.Fatalf("Parse(%q) err = %v, want %q", tt.toks, err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected err: %v", tt.toks, err)
			}
			if got.Help != tt.help {
				t.Fatalf("Parse(%q).Help = %v, want %v", tt.toks, got.Help, tt.help)
			}
			if tt.help {
				return
			}
			if !reflect.DeepEqual(got.Values, tt.want) {
				t.Errorf("Parse(%q).Values = %#v, want %#v", tt.toks, got.Values, tt.want)
			}
		})
	}
}

// TestArgsUsage checks the argument list of the "unexpected argument" hint
// for every form, including list forms that never produce that error.
func TestArgsUsage(t *testing.T) {
	tests := []struct {
		args []Spec
		want string
	}{
		{nil, "no arguments"},
		{[]Spec{{Name: "pod", Required: true}}, "1 argument: <pod>"},
		{[]Spec{{Name: "a", Required: true}, {Name: "b", Default: "x", HasDefault: true}, {Name: "c", Required: true, Variadic: true}}, "3 arguments: <a> [b] <c>..."},
		{[]Spec{{Name: "a", Required: true}, {Name: "rest", Rest: true}}, "2 arguments: <a> [rest...]"},
		{[]Spec{{Name: "paths", Variadic: true}}, "1 argument: [paths...]"},
		{[]Spec{{Name: "k_args", Required: true, Rest: true}}, "1 argument: <k-args>..."},
	}
	for _, tt := range tests {
		if got := argsUsage(tt.args); got != tt.want {
			t.Errorf("argsUsage(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestScan(t *testing.T) {
	t.Run("pending after --tag", func(t *testing.T) {
		st := Scan(dockerBuild.args, dockerBuild.flags, []string{"--tag"})
		if st.Pending == nil || st.Pending.Name != "tag" {
			t.Fatalf("Pending = %+v, want tag", st.Pending)
		}
		if !st.Used["tag"] {
			t.Errorf("Used = %v, want tag", st.Used)
		}
	})
	t.Run("invalid bool value does not mark the flag used", func(t *testing.T) {
		st := Scan(k8sLogs.args, k8sLogs.flags, []string{"--follow=x"})
		if st.Used["follow"] {
			t.Fatalf("Used = %v, want follow unused", st.Used)
		}
	})
	t.Run("valid bool value marks the flag used", func(t *testing.T) {
		st := Scan(k8sLogs.args, k8sLogs.flags, []string{"--follow=off"})
		if !st.Used["follow"] {
			t.Fatalf("Used = %v, want follow", st.Used)
		}
	})
	t.Run("pending resolved by value", func(t *testing.T) {
		st := Scan(dockerBuild.args, dockerBuild.flags, []string{"--tag", "x"})
		if st.Pending != nil {
			t.Fatalf("Pending = %+v, want nil", st.Pending)
		}
	})
	t.Run("in rest after run nginx", func(t *testing.T) {
		st := Scan(dockerRun.args, dockerRun.flags, []string{"nginx"})
		if !st.InRest || st.Positionals != 1 {
			t.Fatalf("InRest = %v, Positionals = %d, want true, 1", st.InRest, st.Positionals)
		}
	})
	t.Run("not in rest after --rm", func(t *testing.T) {
		st := Scan(dockerRun.args, dockerRun.flags, []string{"--rm"})
		if st.InRest || st.Positionals != 0 || !st.Used["rm"] {
			t.Fatalf("got %+v, want not in rest, 0 positionals, rm used", st)
		}
	})
	t.Run("positionals zero after --force", func(t *testing.T) {
		st := Scan(filesCopy.args, filesCopy.flags, []string{"--force"})
		if st.Positionals != 0 || !st.Used["force"] {
			t.Fatalf("got %+v, want 0 positionals, force used", st)
		}
	})
	t.Run("after --", func(t *testing.T) {
		st := Scan(filesCopy.args, filesCopy.flags, []string{"--", "a"})
		if !st.AfterDD || st.Positionals != 1 {
			t.Fatalf("got %+v, want AfterDD, 1 positional", st)
		}
	})
	t.Run("rest only: in rest after --", func(t *testing.T) {
		st := Scan(shortcut.args, shortcut.flags, []string{"--"})
		if !st.AfterDD || !st.InRest {
			t.Fatalf("got %+v, want AfterDD and InRest", st)
		}
	})
	t.Run("continues past help and errors", func(t *testing.T) {
		st := Scan(k8sLogs.args, k8sLogs.flags, []string{"--bogus", "-h", "pod", "--follow"})
		if st.Positionals != 1 || !st.Used["follow"] {
			t.Fatalf("got %+v, want 1 positional, follow used", st)
		}
	})
}

func TestParseErrorsOmitUnrelatedValues(t *testing.T) {
	_, err := Parse(k8sLogs.args, k8sLogs.flags, []string{"--tail=s3cr3t", "--bogus=s3cr3t"}, k8sLogs.label)
	if err == nil || strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("err = %v, want error without the value", err)
	}
}

func TestScanNext(t *testing.T) {
	names := func(specs []*Spec) []string {
		out := []string{}
		for _, s := range specs {
			out = append(out, s.Name)
		}
		return out
	}
	req := func(name string) Spec { return Spec{Name: name, Required: true} }
	variadic := func(name string) Spec { return Spec{Name: name, Required: true, Variadic: true} }
	boolFlag := []Spec{{Name: "v", Type: "bool", Flag: true}}
	tests := []struct {
		name      string
		args      []Spec
		flags     []Spec
		toks      []string
		want      []string
		wantInRst bool
	}{
		{"optional second, none", []Spec{req("a"), {Name: "b"}}, nil, nil, []string{"a"}, false},
		{"optional second, one", []Spec{req("a"), {Name: "b"}}, nil, []string{"x"}, []string{"b"}, false},
		{"optional second, all taken", []Spec{req("a"), {Name: "b"}}, nil, []string{"x", "y"}, []string{}, false},
		{"variadic with tail, none", []Spec{variadic("src"), req("dst")}, nil, nil, []string{"src", "dst"}, false},
		{"variadic with tail, one", []Spec{variadic("src"), req("dst")}, nil, []string{"x"}, []string{"src", "dst"}, false},
		{"variadic with tail, three", []Spec{variadic("src"), req("dst")}, nil, []string{"x", "y", "z"}, []string{"src", "dst"}, false},
		{"head variadic tail, none", []Spec{req("h"), variadic("src"), req("t1"), req("t2")}, nil, nil, []string{"h"}, false},
		{"head variadic tail, one", []Spec{req("h"), variadic("src"), req("t1"), req("t2")}, nil, []string{"x"}, []string{"src", "t1", "t2"}, false},
		{"rest after arg, none", []Spec{req("img"), {Name: "rest", Rest: true}}, nil, nil, []string{"img"}, false},
		{"rest after arg, one", []Spec{req("img"), {Name: "rest", Rest: true}}, nil, []string{"x"}, []string{}, true},
		{"only rest, none", []Spec{{Name: "rest", Rest: true}}, nil, nil, []string{"rest"}, false},
		{"only rest, one", []Spec{{Name: "rest", Rest: true}}, nil, []string{"x"}, []string{}, true},
		{"bool flag only", []Spec{req("a"), req("b")}, boolFlag, []string{"--v"}, []string{"a"}, false},
		{"double dash", []Spec{req("a"), req("b")}, boolFlag, []string{"--"}, []string{"a"}, false},
		{"double dash then word", []Spec{req("a"), req("b")}, boolFlag, []string{"--", "x"}, []string{"b"}, false},
		{"no args", nil, nil, nil, []string{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := Scan(tc.args, tc.flags, tc.toks)
			if got := names(st.Next); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Scan(%q).Next = %v, want %v", tc.toks, got, tc.want)
			}
			if st.InRest != tc.wantInRst {
				t.Errorf("Scan(%q).InRest = %v, want %v", tc.toks, st.InRest, tc.wantInRst)
			}
		})
	}
}
