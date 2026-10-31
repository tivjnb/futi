package argvtmpl

import (
	"reflect"
	"testing"
)

func errStrings(errs []error) []string {
	var out []string
	for _, err := range errs {
		out = append(out, err.Error())
	}
	return out
}

func TestCheckErrors(t *testing.T) {
	var (
		pod      = Input{Name: "pod"}
		host     = Input{Name: "host"}
		user     = Input{Name: "user", Optional: true}
		tail     = Input{Name: "tail", Kind: Int, Flag: true, Optional: true}
		force    = Input{Name: "force", Kind: Bool, Flag: true}
		flagA    = Input{Name: "a", Kind: Bool, Flag: true}
		envs     = Input{Name: "envs", Flag: true, Variadic: true, Optional: true}
		rest     = Input{Name: "rest", Rest: true}
		src      = Input{Name: "src", Variadic: true}
		scalarX  = Input{Name: "x"}
		unusedX  = Input{Name: "x", Optional: true}
		dropHint = `"user" may be unset and would drop the whole argument with "host"; use {{if .user}}{{.user}}@{{end}}{{.host}}`
		x3Hint   = `"--tail" would be left without its value when "tail" is unset; use {{if .tail}}--tail {{.tail}}{{end}} or --tail={{.tail}}`
	)
	tests := []struct {
		name   string
		src    string
		inputs []Input
		want   []string // full Error() of every error, nil for none
	}{
		{"unknown input", `echo {{.x}}`, nil, []string{`1:8: unknown input "x"`}},
		{"unknown input in condition", `{{if .x}}a{{end}}`, nil, []string{`1:6: unknown input "x"`}},
		{"old args", `echo {{.args}}`, nil, []string{`1:8: {{.args}} was replaced by a rest arg (see README)`}},
		{"old args_list", `echo {{.args_list}}`, nil, []string{`1:8: {{.args_list}} was replaced by a rest arg (see README)`}},
		{"declared args is an input", `echo {{.args}}`, []Input{{Name: "args"}}, nil},
		{"bool outside condition", `rm {{.force}}`, []Input{force}, []string{`1:6: bool input "force" can only be used in a condition: {{if .force}}…{{end}}`}},
		{"bool in condition", `rm {{if .force}}-f{{end}} {{if eq .force true}}-r{{end}}`, []Input{force}, nil},
		{"variadic glued", `run --env={{.envs}}`, []Input{envs}, []string{`1:13: variadic input "envs" must be a whole argument; use {{range .env}}--env={{.}}{{end}}`}},
		{"rest glued", `run x{{.rest}}`, []Input{rest}, []string{`1:8: variadic input "rest" must be a whole argument; use {{range .env}}--env={{.}}{{end}}`}},
		{"variadic in quotes", `run "{{.envs}}"`, []Input{envs}, []string{`1:8: variadic input "envs" must be a whole argument; use {{range .env}}--env={{.}}{{end}}`}},
		{"variadic whole argument", `run {{.envs}} {{.rest}}`, []Input{envs, rest}, nil},
		{"variadic in range", `run {{range .envs}}--env={{.}} {{end}}`, []Input{envs}, nil},
		{"range over scalar", `a {{range .x}}{{.}}{{end}}`, []Input{scalarX}, []string{`1:11: range over "x": only variadic inputs can be ranged`}},
		{"range over rest", `a {{range .rest}}{{.}}{{end}}`, []Input{rest}, []string{`1:11: range over "rest": only variadic inputs can be ranged`}},
		{"eq on variadic", `{{if eq .src "a"}}x{{end}} {{.src}}`, []Input{src}, []string{`1:9: eq cannot compare list input "src"`}},
		{"eq on rest in parens", `{{if eq "a" (.rest)}}x{{end}} {{.rest}}`, []Input{rest}, []string{`1:14: eq cannot compare list input "rest"`}},
		{"truth of variadic", `{{if and .src (not .src)}}x{{end}} {{.src}}`, []Input{src}, nil},
		{"optional glued with another input", `ssh {{.user}}@{{.host}}`, []Input{user, host}, []string{"1:7: " + dropHint}},
		{"guarded by another input", `{{if .a}}{{.user}}@{{end}}{{.host}}`, []Input{flagA, user, host}, []string{"1:12: " + dropHint}},
		{"guarded by itself", `{{if .user}}{{.user}}@{{end}}{{.host}}`, []Input{user, host}, nil},
		{"guarded by and", `{{if and .a (and .user)}}{{.user}}@{{end}}{{.host}}`, []Input{flagA, user, host}, nil},
		{"else is not guarded", `{{if .user}}x{{else}}{{.user}}@{{end}}{{.host}}`, []Input{user, host}, []string{"1:24: " + dropHint}},
		{"else if is guarded", `{{if .a}}x{{else if .user}}{{.user}}@{{end}}{{.host}}`, []Input{flagA, user, host}, nil},
		{"or does not guard", `{{if or .a .user}}{{.user}}@{{end}}{{.host}}`, []Input{flagA, user, host}, []string{"1:21: " + dropHint}},
		{"same optional twice", `{{.user}}:{{.user}}`, []Input{user}, nil},
		{"dangling flag", `kubectl logs --tail {{.tail}}`, []Input{tail}, []string{"1:14: " + x3Hint}},
		{"dangling flag inside if", `logs {{if .a}}--tail{{end}} {{.tail}}`, []Input{flagA, tail}, []string{"1:15: " + x3Hint}},
		{"flag with value glued", `kubectl logs --tail={{.tail}}`, []Input{tail}, nil},
		{"flag before required input", `kubectl logs --pod {{.pod}}`, []Input{pod}, nil},
		{"flag guarded", `logs {{if .tail}}--tail {{.tail}}{{end}}`, []Input{tail}, nil},
		{"unused input", `echo`, []Input{unusedX}, []string{`input "x" is not used in cmd`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.src, err)
			}
			if got := errStrings(tmpl.Check(tc.inputs)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Check(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

func TestCheckOrderAndList(t *testing.T) {
	force := Input{Name: "force", Kind: Bool}
	z := Input{Name: "z"}
	tmpl, err := Parse(`{{.b}} {{.force}} {{.a}} {{.b}}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`1:3: unknown input "b"`,
		`1:10: bool input "force" can only be used in a condition: {{if .force}}…{{end}}`,
		`1:21: unknown input "a"`,
		`input "z" is not used in cmd`,
	}
	if got := errStrings(tmpl.Check([]Input{force, z})); !reflect.DeepEqual(got, want) {
		t.Errorf("Check() = %q, want %q", got, want)
	}

	tail := Input{Name: "tail", Kind: Int, Flag: true, Optional: true}
	user := Input{Name: "user", Optional: true}
	host := Input{Name: "host"}
	list, err := ParseList([]string{"--tail", "{{.tail}}", "{{.user}}@{{.host}}", "x", "{{.user}} {{.host}}"})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{
		`[1] 1:1: "--tail" would be left without its value when "tail" is unset; use {{if .tail}}--tail {{.tail}}{{end}} or --tail={{.tail}}`,
		`[3] 1:3: "user" may be unset and would drop the whole argument with "host"; use {{if .user}}{{.user}}@{{end}}{{.host}}`,
		`[5] 1:3: "user" may be unset and would drop the whole argument with "host"; use {{if .user}}{{.user}}@{{end}}{{.host}}`,
	}
	if got := errStrings(list.Check([]Input{tail, user, host})); !reflect.DeepEqual(got, want) {
		t.Errorf("list Check() = %q, want %q", got, want)
	}
}

func TestCheckOK(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		inputs []Input
	}{
		{"S1", `kubectl logs {{if .follow}}-f{{end}} --tail={{.tail}} -- {{.pod}}`, []Input{
			{Name: "pod"},
			{Name: "follow", Kind: Bool, Flag: true},
			{Name: "tail", Kind: Int, Flag: true},
		}},
		{"S10", `cp {{.src}} {{.dst}}`, []Input{
			{Name: "src", Variadic: true},
			{Name: "dst"},
		}},
		{"S12", `grep -- {{.pattern}} {{.paths}}`, []Input{
			{Name: "pattern"},
			{Name: "paths", Variadic: true, Optional: true},
		}},
		{"S27", `cp {{if .force}}-f{{end}} -- {{.src}} {{.dst}}`, []Input{
			{Name: "src"},
			{Name: "dst"},
			{Name: "force", Kind: Bool, Flag: true},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.src, err)
			}
			if got := tmpl.Check(tc.inputs); got != nil {
				t.Errorf("Check(%q) = %q, want nil", tc.src, errStrings(got))
			}
		})
	}
}

func TestCheckHookWithoutInputs(t *testing.T) {
	tmpl, err := Parse(`{{.module_root}}/run {{.module_name}} {{.env.HOME}} {{if .env.X}}-x{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := tmpl.Check(nil); got != nil {
		t.Errorf("Check(nil) = %q, want nil", errStrings(got))
	}
}

func TestGuarded(t *testing.T) {
	s := func(name string) Input { return Input{Name: name} }
	tests := []struct {
		name   string
		src    string
		inputs []Input
		input  string
		want   bool
	}{
		{"after --", `cp -- {{.src}} {{.dst}}`, []Input{s("src"), s("dst")}, "src", false},
		{"after --, last", `cp -- {{.src}} {{.dst}}`, []Input{s("src"), s("dst")}, "dst", false},
		{"after --, inside if", `grep -- {{.pattern}} {{if .x}}{{.paths}}{{end}}`, []Input{s("pattern"), {Name: "x", Kind: Bool}, {Name: "paths", Variadic: true}}, "paths", false},
		{"literal after --", `kubectl exec {{.pod}} -- ls {{.path}}`, []Input{s("pod"), s("path")}, "path", true},
		{"before --", `kubectl exec {{.pod}} -- ls {{.path}}`, []Input{s("pod"), s("path")}, "pod", true},
		{"literal glued after --", `rsync -- {{.src}} {{.host}}:{{.dst}}`, []Input{s("src"), s("host"), s("dst")}, "src", true},
		{"literal tail after --", `ssh {{.host}} -- tail {{.file}}`, []Input{s("host"), s("file")}, "file", true},
		{"allow_dash", `cp {{.src}}`, []Input{{Name: "src", AllowDash: true}}, "src", false},
		{"no exemption", `cp {{.src}}`, []Input{s("src")}, "src", true},
		{"rest", `docker run {{.image}} {{.rest}}`, []Input{s("image"), {Name: "rest", Rest: true}}, "rest", false},
		{"range element", `x {{range .e}}{{.}}{{end}}`, []Input{{Name: "e", Variadic: true}}, "e", true},
		{"range element after --", `x -- {{range .e}}{{.}} {{end}}`, []Input{{Name: "e", Variadic: true}}, "e", false},
		{"two refs adjacent after --", `x -- {{.a}}{{.b}}`, []Input{s("a"), s("b")}, "a", true},
		{"env after --", `x -- {{.a}} {{.env.X}}`, []Input{s("a")}, "a", true},
		{"quoted after --", `x -- "{{.a}}"`, []Input{s("a")}, "a", true},
		{"-- inside if does not exempt", `x {{if .f}}--{{end}} {{.a}}`, []Input{{Name: "f", Kind: Bool}, s("a")}, "a", true},
		{"condition only", `x {{if .a}}y{{end}}`, []Input{s("a")}, "a", false},
		{"unknown input", `cp {{.src}}`, []Input{s("src")}, "nope", false},
		{"literal before value", `--tail={{.tail}}`, []Input{s("tail")}, "tail", false},
		{"conditional literal before value", `{{if .f}}--x={{end}}{{.v}}`, []Input{{Name: "f", Kind: Bool}, s("v")}, "v", true},
		{"literal in else before value", `{{if .f}}x{{else}}--x={{.v}}{{end}}`, []Input{{Name: "f", Kind: Bool}, s("v")}, "v", false},
		{"literal in the same if body", `{{if .f}}--x={{.v}}{{end}}`, []Input{{Name: "f", Kind: Bool}, s("v")}, "v", false},
		{"literal in if before value in else", `{{if .f}}--x={{else}}{{.v}}{{end}}`, []Input{{Name: "f", Kind: Bool}, s("v")}, "v", true},
		{"other input before value", `{{.a}}{{.b}}`, []Input{s("a"), s("b")}, "b", true},
		{"quotes before value", `x "{{.a}}"`, []Input{s("a")}, "a", true},
		{"literal in quotes before value", `x "-o {{.a}}"`, []Input{s("a")}, "a", false},
		{"literal in range body", `x {{range .e}}--env={{.}} {{end}}`, []Input{{Name: "e", Variadic: true}}, "e", false},
		{"one of two occurrences can start", `x --a={{.a}} {{.a}}`, []Input{s("a")}, "a", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.src, err)
			}
			if got := tmpl.Guarded(tc.inputs, tc.input); got != tc.want {
				t.Errorf("Parse(%q).Guarded(%q) = %v, want %v", tc.src, tc.input, got, tc.want)
			}
		})
	}
}

func TestGuardedList(t *testing.T) {
	src := []Input{{Name: "src"}}
	tests := []struct {
		name  string
		items []string
		want  bool
	}{
		{"after --", []string{"cp", "--", "{{.src}}"}, false},
		{"literal after --", []string{"cp", "--", "x", "{{.src}}"}, true},
		{"space is literal in list", []string{"cp", "--", "{{.src}} "}, true},
		{"-- with space is not --", []string{"cp", "-- ", "{{.src}}"}, true},
		{"quote is literal in list", []string{"cp", `"{{.src}}`}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := ParseList(tc.items)
			if err != nil {
				t.Fatalf("ParseList(%q) error: %v", tc.items, err)
			}
			if got := tmpl.Guarded(src, "src"); got != tc.want {
				t.Errorf("ParseList(%q).Guarded(src) = %v, want %v", tc.items, got, tc.want)
			}
		})
	}
}
