package argvtmpl

import "testing"

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
