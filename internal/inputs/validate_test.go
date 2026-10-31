package inputs

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

func str(s string) *string { return &s }

const formMsg = "args must be: required…, then optional…; or required…, one variadic, required…; or required…, rest"

func TestValidate(t *testing.T) {
	tests := []struct {
		name  string
		args  []Spec
		flags []Spec
		want  []string // exact error messages, in order
	}{
		{
			name:  "k8s logs",
			args:  []Spec{{Name: "pod", Required: true}},
			flags: []Spec{{Name: "follow", Type: "bool", Short: "f", Flag: true}, {Name: "tail", Type: "int", Default: str("100"), Flag: true}},
		},
		{
			name:  "files copy: variadic then required",
			args:  []Spec{{Name: "src", Required: true, Variadic: true}, {Name: "dst", Required: true}},
			flags: []Spec{{Name: "force", Type: "bool", Flag: true}},
		},
		{
			name:  "files grep: required then optional variadic",
			args:  []Spec{{Name: "pattern", Required: true}, {Name: "paths", Variadic: true}},
			flags: []Spec{{Name: "ignore_case", Type: "bool", Short: "i", Flag: true}},
		},
		{
			name:  "docker build: required flag, optional arg with default",
			args:  []Spec{{Name: "context", Default: str(".")}},
			flags: []Spec{{Name: "tag", Required: true, Flag: true}, {Name: "file", Default: str("Dockerfile"), Flag: true}},
		},
		{
			name: "docker run: required then rest",
			args: []Spec{{Name: "image", Required: true}, {Name: "extra", Rest: true}},
		},
		{
			name: "invalid name",
			args: []Spec{{Name: "Pod"}},
			want: []string{`arg "Pod": invalid name "Pod" (allowed: a-z, 0-9, _; starts with a letter)`},
		},
		{
			name:  "invalid flag name with hyphen",
			flags: []Spec{{Name: "a-b", Flag: true}},
			want:  []string{`flag "a-b": invalid name "a-b" (allowed: a-z, 0-9, _; starts with a letter)`},
		},
		{
			name: "reserved name",
			args: []Spec{{Name: "env"}},
			want: []string{`arg "env": name "env" is reserved`},
		},
		{
			name: "reserved help",
			args: []Spec{{Name: "help"}},
			want: []string{`arg "help": name "help" is reserved`},
		},
		{
			name:  "reserved module_root",
			flags: []Spec{{Name: "module_root", Flag: true}},
			want:  []string{`flag "module_root": name "module_root" is reserved`},
		},
		{
			name: "reserved module_name",
			args: []Spec{{Name: "module_name"}},
			want: []string{`arg "module_name": name "module_name" is reserved`},
		},
		{
			name: "duplicate among args",
			args: []Spec{{Name: "x"}, {Name: "x"}},
			want: []string{`arg "x": duplicate name "x"`},
		},
		{
			name:  "duplicate among flags",
			flags: []Spec{{Name: "x", Flag: true}, {Name: "x", Flag: true}},
			want:  []string{`flag "x": duplicate name "x"`},
		},
		{
			name:  "duplicate across args and flags",
			args:  []Spec{{Name: "x"}},
			flags: []Spec{{Name: "x", Flag: true}},
			want:  []string{`flag "x": duplicate name "x"`},
		},
		{
			name: "invalid type",
			args: []Spec{{Name: "x", Type: "float"}},
			want: []string{`arg "x": invalid type "float" (valid: string, int, bool)`},
		},
		{
			name: "bool arg",
			args: []Spec{{Name: "x", Type: "bool"}},
			want: []string{`arg "x": bool is only allowed for flags`},
		},
		{
			name:  "required bool",
			flags: []Spec{{Name: "x", Type: "bool", Required: true, Flag: true}},
			want:  []string{`flag "x": required is not allowed for bool`},
		},
		{
			name: "required and default",
			args: []Spec{{Name: "x", Required: true, Default: str("a")}},
			want: []string{`arg "x": required and default are mutually exclusive`},
		},
		{
			name: "required with empty default is fine",
			args: []Spec{{Name: "x", Required: true, Default: str("")}},
		},
		{
			name: "int default not a number",
			args: []Spec{{Name: "x", Type: "int", Default: str("1.5")}},
			want: []string{`arg "x": default is not an integer`},
		},
		{
			name:  "bool default invalid",
			flags: []Spec{{Name: "x", Type: "bool", Default: str("maybe"), Flag: true}},
			want:  []string{`flag "x": default is not a boolean (true/false/1/0/yes/no/on/off)`},
		},
		{
			name:  "bool default case-insensitive",
			flags: []Spec{{Name: "x", Type: "bool", Default: str("YES"), Flag: true}},
		},
		{
			name: "variadic default",
			args: []Spec{{Name: "x", Variadic: true, Default: str("a")}},
			want: []string{`arg "x": variadic and rest inputs cannot have a default`},
		},
		{
			name: "rest default",
			args: []Spec{{Name: "x", Rest: true, Default: str("a")}},
			want: []string{`arg "x": variadic and rest inputs cannot have a default`},
		},
		{
			name:  "short ok on repeatable flag",
			flags: []Spec{{Name: "x", Short: "F", Variadic: true, Flag: true}},
		},
		{
			name:  "short not a letter",
			flags: []Spec{{Name: "x", Short: "1", Flag: true}},
			want:  []string{`flag "x": invalid short "1" (must be one ASCII letter)`},
		},
		{
			name:  "short too long",
			flags: []Spec{{Name: "x", Short: "ab", Flag: true}},
			want:  []string{`flag "x": invalid short "ab" (must be one ASCII letter)`},
		},
		{
			name:  "short h reserved",
			flags: []Spec{{Name: "x", Short: "h", Flag: true}},
			want:  []string{`flag "x": short "h" is reserved`},
		},
		{
			name:  "short duplicate, case significant",
			flags: []Spec{{Name: "a", Short: "f", Flag: true}, {Name: "b", Short: "f", Flag: true}, {Name: "c", Short: "F", Flag: true}},
			want:  []string{`flag "b": duplicate short "f"`},
		},
		{
			name: "short on arg",
			args: []Spec{{Name: "x", Short: "x"}},
			want: []string{`arg "x": short is only allowed for flags`},
		},
		{
			name:  "variadic bool",
			flags: []Spec{{Name: "x", Type: "bool", Variadic: true, Flag: true}},
			want:  []string{`flag "x": variadic is not allowed for bool`},
		},
		{
			name:  "rest flag",
			flags: []Spec{{Name: "x", Rest: true, Flag: true}},
			want:  []string{`flag "x": rest is only allowed for args`},
		},
		{
			name: "rest not last",
			args: []Spec{{Name: "x", Rest: true}, {Name: "y", Required: true}},
			want: []string{`arg "x": rest must be the last arg`},
		},
		{
			name: "variadic and rest",
			args: []Spec{{Name: "x", Variadic: true, Rest: true}},
			want: []string{`arg "x": variadic and rest cannot be combined`},
		},
		{
			name:  "allow_dash bool",
			flags: []Spec{{Name: "x", Type: "bool", AllowDash: true, Flag: true}},
			want:  []string{`flag "x": allow_dash is not allowed for bool`},
		},
		{
			name: "form: required after optional",
			args: []Spec{{Name: "a"}, {Name: "b", Required: true}},
			want: []string{formMsg},
		},
		{
			name: "form: optional before variadic",
			args: []Spec{{Name: "a"}, {Name: "b", Variadic: true}},
			want: []string{formMsg},
		},
		{
			name: "form: two variadics",
			args: []Spec{{Name: "a", Variadic: true}, {Name: "b", Variadic: true}},
			want: []string{formMsg},
		},
		{
			name: "form: optional after variadic",
			args: []Spec{{Name: "a", Variadic: true}, {Name: "b"}},
			want: []string{formMsg},
		},
		{
			name: "form: optional before rest",
			args: []Spec{{Name: "a"}, {Name: "r", Rest: true}},
			want: []string{formMsg},
		},
		{
			name: "form: optional after rest is reported once",
			args: []Spec{{Name: "r", Rest: true}, {Name: "a"}},
			want: []string{`arg "r": rest must be the last arg`},
		},
		{
			name: "form: required after rest is reported once",
			args: []Spec{{Name: "r", Rest: true}, {Name: "a", Required: true}},
			want: []string{`arg "r": rest must be the last arg`},
		},
		{
			name: "required rest after required",
			args: []Spec{{Name: "a", Required: true}, {Name: "r", Rest: true, Required: true}},
		},
		{
			name: "required variadic alone",
			args: []Spec{{Name: "v", Variadic: true, Required: true}},
		},
		{
			name: "form: one error per node",
			args: []Spec{{Name: "a"}, {Name: "b", Required: true}, {Name: "c"}, {Name: "d", Required: true}},
			want: []string{formMsg},
		},
		{
			name: "per-input errors then form error",
			args: []Spec{{Name: "A"}, {Name: "b", Required: true}},
			want: []string{
				`arg "A": invalid name "A" (allowed: a-z, 0-9, _; starts with a letter)`,
				formMsg,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, err := range Validate(tc.args, tc.flags) {
				got = append(got, err.Error())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Validate() =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

func TestSpecAccessors(t *testing.T) {
	tests := []struct {
		spec     Spec
		kind     argvtmpl.Kind
		cli      string
		optional bool
	}{
		{Spec{Name: "pod", Required: true}, argvtmpl.String, "<pod>", false},
		{Spec{Name: "ignore_case", Type: "bool", Flag: true}, argvtmpl.Bool, "--ignore-case", true},
		{Spec{Name: "tail", Type: "int", Default: str("1"), Flag: true}, argvtmpl.Int, "--tail", false},
		{Spec{Name: "x", Type: "string"}, argvtmpl.String, "<x>", true},
	}
	for _, tc := range tests {
		if got := tc.spec.Kind(); got != tc.kind {
			t.Errorf("%s Kind() = %v, want %v", tc.spec.Name, got, tc.kind)
		}
		if got := tc.spec.CLIName(); got != tc.cli {
			t.Errorf("CLIName() = %q, want %q", got, tc.cli)
		}
		if got := tc.spec.Optional(); got != tc.optional {
			t.Errorf("%s Optional() = %v, want %v", tc.spec.Name, got, tc.optional)
		}
	}
}

func TestTemplateInputs(t *testing.T) {
	args := []Spec{{Name: "image", Required: true}, {Name: "extra", Rest: true, AllowDash: true}}
	flags := []Spec{
		{Name: "dry", Type: "bool", Flag: true},
		{Name: "tag", Variadic: true, Flag: true},
	}
	want := []argvtmpl.Input{
		{Name: "image", Kind: argvtmpl.String},
		{Name: "extra", Kind: argvtmpl.String, Variadic: true, Rest: true, Optional: true, AllowDash: true},
		{Name: "dry", Kind: argvtmpl.Bool, Flag: true},
		{Name: "tag", Kind: argvtmpl.String, Flag: true, Variadic: true, Optional: true},
	}
	got := TemplateInputs(args, flags)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TemplateInputs() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestValidateErrorsHaveNoValues(t *testing.T) {
	errs := Validate([]Spec{{Name: "x", Type: "int", Default: str("secret")}}, nil)
	for _, err := range errs {
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("error leaks default value: %v", err)
		}
	}
}
