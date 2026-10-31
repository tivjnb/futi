package argvtmpl

import (
	"reflect"
	"testing"
)

// FuzzEvalValueIsOneArg checks that a value never changes the number of argv
// elements and never leaks into the literal text around it.
func FuzzEvalValueIsOneArg(f *testing.F) {
	for _, seed := range [][2]string{
		{"", ""}, {"a b", "c d"}, {`"`, `'`}, {"-rf", "--"}, {"{{.y}}", "\n\t "}, {`\"`, `\\`},
	} {
		f.Add(seed[0], seed[1])
	}
	tmpl, err := Parse(`a {{.x}} "b {{.y}}"`)
	if err != nil {
		f.Fatal(err)
	}
	inputs := []Input{{Name: "x"}, {Name: "y"}}
	f.Fuzz(func(t *testing.T, x, y string) {
		args, err := tmpl.Eval(inputs, Values{Inputs: map[string]Value{"x": {Scalar: x}, "y": {Scalar: y}}, Env: noEnv})
		if err != nil {
			t.Fatalf("Eval error: %v", err)
		}
		got := Argv(args)
		if len(got) != 3 || got[0] != "a" || got[1] != x || got[2] != "b "+y {
			t.Fatalf("Eval(x=%q, y=%q) = %q, want [a, x, b y]", x, y, got)
		}
	})
}

// FuzzEvalListValuesVerbatim checks that the elements of a variadic input are
// passed one by one with any whitespace inside them, whole or ranged, in the
// string form and in the list form.
func FuzzEvalListValuesVerbatim(f *testing.F) {
	for _, seed := range [][2]string{
		{"", ""}, {"a b", "c\nd"}, {"\n", "\t "}, {" ", "  "}, {`"`, `'`}, {"{{.xs}}", "-e"},
	} {
		f.Add(seed[0], seed[1])
	}
	str, err := Parse(`run {{.xs}} {{range .xs}}-e {{.}} {{end}}`)
	if err != nil {
		f.Fatal(err)
	}
	list, err := ParseList([]string{"run", "{{.xs}}", "{{range .xs}}-e={{.}}{{end}}"})
	if err != nil {
		f.Fatal(err)
	}
	inputs := []Input{{Name: "xs", Variadic: true}}
	f.Fuzz(func(t *testing.T, a, b string) {
		values := Values{Inputs: map[string]Value{"xs": {List: []string{a, b}}}, Env: noEnv}
		for _, tc := range []struct {
			name string
			tmpl *Template
			want []string
		}{
			{"string form", str, []string{"run", a, b, "-e", a, "-e", b}},
			{"list form", list, []string{"run", a, b, "-e=" + a, "-e=" + b}},
		} {
			args, err := tc.tmpl.Eval(inputs, values)
			if err != nil {
				t.Fatalf("%s: Eval error: %v", tc.name, err)
			}
			if got := Argv(args); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s: Eval(a=%q, b=%q) = %q, want %q", tc.name, a, b, got, tc.want)
			}
		}
	})
}
