package inputs

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

func sc(s string) argvtmpl.Value    { return argvtmpl.Value{Scalar: s} }
func ls(s ...string) argvtmpl.Value { return argvtmpl.Value{List: s} }

func TestBind(t *testing.T) {
	tests := []struct {
		name  string
		node  node
		given map[string]any
		want  map[string]argvtmpl.Value
		err   string
	}{
		{"unset bool is false, default applied", k8sLogs, map[string]any{"pod": "p"},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("100")}, ""},
		{"int canonicalized", k8sLogs, map[string]any{"pod": "p", "tail": "007"},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("7")}, ""},
		{"negative int", k8sLogs, map[string]any{"pod": "p", "tail": "-5"},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("-5")}, ""},
		{"bool from yes", k8sLogs, map[string]any{"pod": "p", "follow": "yes"},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("true"), "tail": sc("100")}, ""},
		{"bool from Go bool", k8sLogs, map[string]any{"pod": "p", "follow": true},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("true"), "tail": sc("100")}, ""},
		{"Go false bool", k8sLogs, map[string]any{"pod": "p", "follow": false},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("100")}, ""},
		{"invalid bool", k8sLogs, map[string]any{"pod": "p", "follow": "maybe"}, nil,
			`invalid bool value "maybe" for --follow (valid: true, false, 1, 0, yes, no, on, off)`},
		{"invalid int", k8sLogs, map[string]any{"pod": "p", "tail": "x"}, nil, `invalid int value "x" for --tail`},
		{"empty int", k8sLogs, map[string]any{"pod": "p", "tail": ""}, nil, `empty value for --tail`},
		{"missing arg", k8sLogs, map[string]any{}, nil, `missing argument <pod>`},
		{"empty required arg", k8sLogs, map[string]any{"pod": ""}, nil, `empty value for <pod>`},
		{"missing flag", dockerBuild, map[string]any{}, nil, `missing required flag --tag`},
		{"empty required flag", dockerBuild, map[string]any{"tag": ""}, nil, `empty value for --tag`},
		{"defaults for args and flags", dockerBuild, map[string]any{"tag": "x"},
			map[string]argvtmpl.Value{"tag": sc("x"), "file": sc("Dockerfile"), "context": sc(".")}, ""},
		{"optional string empty is a value", gitCommit, map[string]any{"message": ""},
			map[string]argvtmpl.Value{"message": sc(""), "amend": sc("false")}, ""},
		{"optional string unset has no key", gitCommit, map[string]any{},
			map[string]argvtmpl.Value{"amend": sc("false")}, ""},
		{"variadic from []string", filesCopy, map[string]any{"src": []string{"a", "b"}, "dst": "d"},
			map[string]argvtmpl.Value{"src": ls("a", "b"), "dst": sc("d"), "force": sc("false")}, ""},
		{"variadic from []any", filesCopy, map[string]any{"src": []any{"a"}, "dst": "d"},
			map[string]argvtmpl.Value{"src": ls("a"), "dst": sc("d"), "force": sc("false")}, ""},
		{"required variadic with no values", filesCopy, map[string]any{"src": []string{}, "dst": "d"}, nil, `missing argument <src>`},
		{"required variadic unset", filesCopy, map[string]any{"dst": "d"}, nil, `missing argument <src>`},
		{"optional variadic empty has no key", filesGrep, map[string]any{"pattern": "p", "paths": []string{}},
			map[string]argvtmpl.Value{"pattern": sc("p"), "ignore_case": sc("false"), "follow": sc("false")}, ""},
		{"rest is a list", dockerRun, map[string]any{"image": "nginx", "extra": []string{"-it", "--rm"}},
			map[string]argvtmpl.Value{"image": sc("nginx"), "extra": ls("-it", "--rm"), "rm": sc("false")}, ""},
		{"list element not a string", filesCopy, map[string]any{"src": []any{"a", 1}, "dst": "d"}, nil, `input "src" must be a list of strings`},
		{"unknown key", k8sLogs, map[string]any{"pod": "p", "nope": "1"}, nil, `unknown input "nope"`},
		{"list for scalar", k8sLogs, map[string]any{"pod": []string{"a"}}, nil, `input "pod" must be a single value`},
		{"scalar for list", filesCopy, map[string]any{"src": "a", "dst": "d"}, nil, `input "src" must be a list`},
		{"bool for string", k8sLogs, map[string]any{"pod": true}, nil, `input "pod" must be a string`},
		{"int from float64", k8sLogs, map[string]any{"pod": "p", "tail": float64(7)},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("7")}, ""},
		{"int from Go int", k8sLogs, map[string]any{"pod": "p", "tail": 8},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("8")}, ""},
		{"int from int64", k8sLogs, map[string]any{"pod": "p", "tail": int64(9)},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("9")}, ""},
		{"int from json.Number", k8sLogs, map[string]any{"pod": "p", "tail": json.Number("007")},
			map[string]argvtmpl.Value{"pod": sc("p"), "follow": sc("false"), "tail": sc("7")}, ""},
		{"fractional float64 for int", k8sLogs, map[string]any{"pod": "p", "tail": 1.5}, nil, `invalid int value "1.5" for --tail`},
		{"number for string", k8sLogs, map[string]any{"pod": float64(1)}, nil, `input "pod" must be a string`},
		{"number for bool", k8sLogs, map[string]any{"pod": "p", "follow": float64(1)}, nil, `input "follow" must be a string`},
		{"required with default takes the default", node{args: []Spec{{Name: "a", Required: true, Default: "x", HasDefault: true}}}, map[string]any{},
			map[string]argvtmpl.Value{"a": sc("x")}, ""},
		{"int variadic canonicalized", portsNode, map[string]any{"port": []string{"08", "-3"}},
			map[string]argvtmpl.Value{"port": ls("8", "-3")}, ""},
		{"int variadic from []any canonicalized", portsNode, map[string]any{"port": []any{"08"}},
			map[string]argvtmpl.Value{"port": ls("8")}, ""},
		{"int variadic invalid element", portsNode, map[string]any{"port": []string{"abc", "08"}}, nil, `invalid int value "abc" for --port`},
		{"int variadic empty element", portsNode, map[string]any{"port": []any{"1", ""}}, nil, `empty value for --port`},
		{"rest keeps strings as is", dockerRun, map[string]any{"image": "i", "extra": []string{"08", ""}},
			map[string]argvtmpl.Value{"image": sc("i"), "extra": ls("08", ""), "rm": sc("false")}, ""},
		{"unknown key wins over missing", k8sLogs, map[string]any{"nope": "1"}, nil, `unknown input "nope"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Bind(tc.node.args, tc.node.flags, tc.given)
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

var portsNode = node{label: "futi ports", flags: []Spec{{Name: "port", Type: "int", Variadic: true, Flag: true}}}

func TestParseThenBindIntVariadic(t *testing.T) {
	res, err := Parse(portsNode.args, portsNode.flags, []string{"--port", "abc", "--port", "08"}, portsNode.label)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Bind(portsNode.args, portsNode.flags, res.Values)
	if err == nil || err.Error() != `invalid int value "abc" for --port` {
		t.Fatalf("err = %v, want invalid int value", err)
	}
	res, err = Parse(portsNode.args, portsNode.flags, []string{"--port", "08", "--port", "9"}, portsNode.label)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := Bind(portsNode.args, portsNode.flags, res.Values)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if want := ls("8", "9"); !reflect.DeepEqual(got["port"], want) {
		t.Fatalf("port = %#v, want %#v", got["port"], want)
	}
}

func TestBindChoices(t *testing.T) {
	level := node{flags: []Spec{{Name: "level", Flag: true, Choices: []string{"debug", "info"}}}}
	reqLevel := node{flags: []Spec{{Name: "level", Flag: true, Required: true, Choices: []string{"debug", "info"}}}}
	count := node{flags: []Spec{{Name: "n", Type: "int", Flag: true, Choices: []string{"3"}}}}
	items := node{args: []Spec{{Name: "item", Variadic: true, Choices: []string{"a", "b"}}}}
	file := node{flags: []Spec{{Name: "path", Type: "file", Flag: true, Choices: []string{"x"}}}}
	pathNode := node{flags: []Spec{{Name: "path", Type: "file", Flag: true}}}
	tests := []struct {
		name  string
		node  node
		given map[string]any
		want  map[string]argvtmpl.Value
		err   string // exact; "!invalid value" means: an error without that text
	}{
		{"allowed", level, map[string]any{"level": "info"}, map[string]argvtmpl.Value{"level": sc("info")}, ""},
		{"case differs", level, map[string]any{"level": "Info"}, nil, `invalid value "Info" for --level (valid: debug, info)`},
		{"int canonical match", count, map[string]any{"n": "03"}, map[string]argvtmpl.Value{"n": sc("3")}, ""},
		{"int from float64", count, map[string]any{"n": float64(3)}, map[string]argvtmpl.Value{"n": sc("3")}, ""},
		{"int not in choices", count, map[string]any{"n": "4"}, nil, `invalid value "4" for --n (valid: 3)`},
		{"fractional float64 is a type error", count, map[string]any{"n": float64(3.5)}, nil, "!invalid value"},
		{"required empty", reqLevel, map[string]any{"level": ""}, nil, `empty value for --level`},
		{"optional empty is not a choice", level, map[string]any{"level": ""}, nil, `invalid value "" for --level (valid: debug, info)`},
		{"variadic element", items, map[string]any{"item": []any{"a", "c"}}, nil, `invalid value "c" for <item> (valid: a, b)`},
		{"variadic ok", items, map[string]any{"item": []string{"b", "a"}}, map[string]argvtmpl.Value{"item": ls("b", "a")}, ""},
		{"file choice is compared as is", file, map[string]any{"path": "y"}, nil, `invalid value "y" for --path (valid: x)`},
		{"file without choices is not touched", pathNode, map[string]any{"path": "a b/../x"}, map[string]argvtmpl.Value{"path": sc("a b/../x")}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Bind(tc.node.args, tc.node.flags, tc.given)
			if tc.err != "" {
				if err == nil {
					t.Fatalf("err = nil, want %q", tc.err)
				}
				if tc.err == "!invalid value" {
					if strings.Contains(err.Error(), "invalid value") {
						t.Fatalf("err = %v, want a type error", err)
					}
					return
				}
				if err.Error() != tc.err {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestParseThenBindChoices(t *testing.T) {
	n := node{flags: []Spec{{Name: "level", Flag: true, Choices: []string{"debug", "info"}}}}
	res, err := Parse(n.args, n.flags, []string{"--level", "Info"}, "futi x")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Bind(n.args, n.flags, res.Values)
	if want := `invalid value "Info" for --level (valid: debug, info)`; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
