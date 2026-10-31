package inputs

import (
	"reflect"
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
