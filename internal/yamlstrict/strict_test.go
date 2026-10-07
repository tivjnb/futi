package yamlstrict

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

type inner struct {
	Name string `yaml:"name"`
}

type embedded struct {
	Shared string `yaml:"shared"`
}

type sample struct {
	Title    string           `yaml:"title"`
	Skipped  string           `yaml:"-"`
	NoTag    string           // имя ключа — "notag"
	Items    []inner          `yaml:"items"`
	ByName   map[string]inner `yaml:"by_name"`
	Ptr      *inner           `yaml:"ptr"`
	Opts     inner            `yaml:"opts,omitempty"`
	Scalar   string           `yaml:"scalar"`
	embedded `yaml:",inline"`
}

// nest — рекурсивный тип: элементы списка того же типа, обход идёт по алиасам.
type nest []nest

type tree struct {
	Commands map[string]tree `yaml:"commands"`
	Cmd      string          `yaml:"cmd"`
}

func check(t *testing.T, src string, typ reflect.Type) []string {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(src), &node); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	var got []string
	for _, e := range UnknownKeys(&node, typ) {
		got = append(got, e.Error())
	}
	return got
}

func TestUnknownKeys(t *testing.T) {
	typ := reflect.TypeOf(sample{})
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"empty document", ``, nil},
		{"only comment", "# nothing\n", nil},
		{"all known", "title: a\nnotag: b\nshared: c\nitems:\n  - name: x\n", nil},
		{"top level", "titel: a\n", []string{`line 1: unknown key "titel"`}},
		{"in struct field", "opts:\n  nme: a\n", []string{`line 2: unknown key "nme" in opts`}},
		{"in slice element", "items:\n  - name: a\n  - nme: b\n", []string{`line 3: unknown key "nme" in items[1]`}},
		{"in map value", "by_name:\n  foo:\n    nme: a\n", []string{`line 3: unknown key "nme" in by_name.foo`}},
		{"in pointer", "ptr:\n  nme: a\n", []string{`line 2: unknown key "nme" in ptr`}},
		{"skipped field is unknown", "Skipped: a\n", []string{`line 1: unknown key "Skipped"`}},
		{"inline field is known", "shared: a\n", nil},
		{"x- top level", "x-anything: [1, 2]\ntitle: a\n", nil},
		{"x- nested", "opts:\n  x-note: hi\n  name: a\n", nil},
		{"x- in slice element", "items:\n  - x-note: hi\n", nil},
		{"map keys are free incl x-", "by_name:\n  x-foo:\n    name: a\n  bar: {name: b}\n", nil},
		{"map x- key value still checked", "by_name:\n  x-foo:\n    nme: a\n", []string{`line 3: unknown key "nme" in by_name.x-foo`}},
		{"scalar mismatch ignored", "items: 5\nopts: text\n", nil},
		{"document order", "aaa: 1\nitems:\n  - bbb: 2\nccc: 3\n", []string{
			`line 1: unknown key "aaa"`,
			`line 3: unknown key "bbb" in items[0]`,
			`line 4: unknown key "ccc"`,
		}},
		{"alias to bad anchor", "x-base: &b {nme: a}\nopts: *b\n", []string{`line 1: unknown key "nme" in opts`}},
		{"alias to good anchor", "x-base: &b {name: a}\nopts: *b\n", nil},
		{"merge bad base", "x-base: &b {bad: a, name: n}\nopts:\n  <<: *b\n", []string{`line 1: unknown key "bad" in opts`}},
		{"merge good base", "x-base: &b {name: a}\nopts:\n  <<: *b\n  name: z\n", nil},
		{"merge list of bases", "x-a: &a {name: a}\nx-b: &b {bad: 1}\nopts:\n  <<: [*a, *b]\n", []string{`line 2: unknown key "bad" in opts`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := check(t, tc.src, typ)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("UnknownKeys(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

func TestUnknownKeysPointerTarget(t *testing.T) {
	got := check(t, "titel: a\n", reflect.TypeOf(&sample{}))
	want := []string{`line 1: unknown key "titel"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnknownKeysZeroNode(t *testing.T) {
	if errs := UnknownKeys(&yaml.Node{}, reflect.TypeOf(sample{})); errs != nil {
		t.Fatalf("zero node: got %v, want nil", errs)
	}
}

func TestUnknownKeysRecursiveType(t *testing.T) {
	src := "commands:\n  build:\n    cmd: x\n    commands:\n      push:\n        cmd: y\n        cm: z\n"
	got := check(t, src, reflect.TypeOf(tree{}))
	want := []string{`line 7: unknown key "cm" in commands.build.commands.push`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Якорь, на который ссылаются экспоненциально много раз, проверяется один раз.
func TestUnknownKeysAliasBombTerminates(t *testing.T) {
	src := "a: &a [x, x]\nb: &b [*a, *a]\nc: &c [*b, *b]\nd: &d [*c, *c]\ne: &e [*d, *d]\nf: &f [*e, *e]\ng: &g [*f, *f]\nh: &h [*g, *g]\n" +
		"i: &i [*h, *h]\nj: &j [*i, *i]\nk: &k [*j, *j]\nl: &l [*k, *k]\nm: &m [*l, *l]\nn: &n [*m, *m]\no: &o [*n, *n]\np: &p [*o, *o]\n" +
		"q: &q [*p, *p]\nr: &r [*q, *q]\ns: &s [*r, *r]\nt: &t [*s, *s]\nu: &u [*t, *t]\nv: &v [*u, *u]\nw: &w [*v, *v]\nx: &x [*w, *w]\n" +
		"y: &y [*x, *x]\nz: &z [*y, *y]\n"
	type bomb struct {
		A, B, C, D, E, F, G, H, I, J, K, L, M, N, O, P, Q, R, S, T, U, V, W, X, Y, Z nest
	}
	got := check(t, src, reflect.TypeOf(bomb{}))
	if len(got) != 0 {
		t.Fatalf("got %q, want no errors", got)
	}
}
