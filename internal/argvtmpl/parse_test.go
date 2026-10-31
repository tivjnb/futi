package argvtmpl

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseAccepts(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string // Refs()
	}{
		{"spec example", `kubectl logs {{if .follow}}-f{{end}} --tail={{.tail}} -- {{.pod}}`, []string{"follow", "pod", "tail"}},
		{"empty", ``, nil},
		{"if else if else", `{{if eq .a "x"}}A{{else if .b}}B{{else}}C{{end}}`, []string{"a", "b"}},
		{"and not with parens", `{{if and .b (not .c)}}x{{end}}`, []string{"b", "c"}},
		{"or with literals", `{{if or (eq .n 3) (eq .m true)}}x{{end}}`, []string{"m", "n"}},
		{"range over input", `docker run {{range .envs}}-e {{.}} {{end}}img`, []string{"envs"}},
		{"env and module vars", `{{.env.HOME}} {{.module_root}}/{{.module_name}}`, nil},
		{"env in condition", `{{if .env.DEBUG}}-v{{end}}`, nil},
		{"single quotes", `'a b'{{.x}}`, []string{"x"}},
		{"double quotes with action", `"{{.a}} x"`, []string{"a"}},
		{"escaped quote in double quotes", `"a \" {{.a}}" b`, []string{"a"}},
		{"quote closed inside body that opened it", `{{if .x}}"a b"{{end}} c`, []string{"x"}},
		{"other quote char inside quotes is literal", `"it's {{if .x}}y{{end}}"`, []string{"x"}},
		{"multiline", "a\n  b {{.x}}\n\tc", []string{"x"}},
		{"comment between spaces", `a {{/* note */}} b`, nil},
		{"comment at edges", "{{/* note */}} a {{/* tail */}}", nil},
		{"negative number is not a trim marker", `{{if eq .n -3}}x{{end}}`, []string{"n"}},
		{"refs deduplicated and sorted", `{{.b}} {{.a}} {{.b}}`, []string{"a", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.src, err)
			}
			if got := tmpl.Refs(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q).Refs() = %#v, want %#v", tc.src, got, tc.want)
			}
		})
	}
}

func TestParseListAccepts(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  []string
	}{
		{"mixed", []string{"a b", "{{.x}}", ""}, []string{"x"}},
		{"quotes are literal", []string{`"a`, `it's`, `{{.y}}'`}, []string{"y"}},
		{"refs across items", []string{"{{.b}}", "{{if .a}}{{.c}}{{end}}", "{{.b}}"}, []string{"a", "b", "c"}},
		{"empty list", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := ParseList(tc.items)
			if err != nil {
				t.Fatalf("ParseList(%q) error: %v", tc.items, err)
			}
			if got := tmpl.Refs(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseList(%q).Refs() = %#v, want %#v", tc.items, got, tc.want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // substring of Error.Msg
	}{
		{"with", `{{with .x}}a{{end}}`, "with"},
		{"define", `{{define "a"}}x{{end}}`, "define"},
		{"block", `{{block "a" .}}x{{end}}`, "block"},
		{"template", `{{template "a"}}`, "template"},
		{"break", `{{range .xs}}{{break}}{{end}}`, "break"},
		{"break outside range", `{{break}}`, "break"},
		{"continue", `{{range .xs}}{{continue}}{{end}}`, "continue"},
		{"variable declaration", `{{$x := .a}}`, "variable"},
		{"variable use", `{{$x}}`, "variable"},
		{"pipe", `{{.x | printf "%s"}}`, "|"},
		{"range with variables", `{{range $i, $v := .xs}}{{$v}}{{end}}`, "range $i, $v"},
		{"range else", `{{range .xs}}a{{else}}b{{end}}`, "range"},
		{"function outside condition", `{{printf "%s" .x}}`, "function"},
		{"quote removed", `{{quote .x}}`, "quote was removed: a value is always exactly one argument"},
		{"unknown function in condition", `{{if len .x}}a{{end}}`, "len"},
		{"quote in condition", `{{if quote .x}}a{{end}}`, "quote was removed"},
		{"nested field", `{{.x.y}}`, ".x.y"},
		{"nested env field", `{{.env.A.B}}`, ".env.A.B"},
		{"bare env", `{{.env}}`, "env"},
		{"range bare env", `{{range .env}}-e {{.}} {{end}}`, "env"},
		{"range over env var", `{{range .env.X}}a{{end}}`, "range"},
		{"dot outside range", `{{.}}`, "."},
		{"dot in condition", `{{range .xs}}{{if .}}a{{end}}{{end}}`, "."},
		{"nil", `{{if eq .x nil}}a{{end}}`, "nil"},
		{"left trim marker", `{{- .x}}`, "trim markers {{- -}} are not supported"},
		{"right trim marker", `{{.x -}}`, "trim markers {{- -}} are not supported"},
		{"left trim marker with newline", "a\n{{-\n.x}}", "trim markers {{- -}} are not supported"},
		{"comment inside argument", `a{{/*c*/}}b`, "comment inside an argument"},
		{"comment inside quotes", `"a {{/*c*/}} b"`, "comment inside an argument"},
		{"comment next to action", `{{.x}}{{/*c*/}} b`, "comment inside an argument"},
		{"quote opened in body", `{{if .x}}"a{{end}} b"`, "unterminated quote"},
		{"quote closed in body", `"a {{if .x}}b" c{{end}} d"`, "unterminated quote"},
		{"quote opened in range body", `{{range .xs}}'a{{end}}'`, "unterminated quote"},
		{"quote opened in else", `{{if .x}}a{{else}}'b{{end}}'`, "unterminated quote"},
		{"unterminated double", `"abc`, "unterminated quote"},
		{"unterminated single", `a 'b {{.x}}`, "unterminated quote"},
		{"escaped quote does not close", `"abc\"`, "unterminated quote"},
		{"eq arity", `{{if eq .x}}a{{end}}`, "eq"},
		{"not arity", `{{if not .x .y}}a{{end}}`, "not"},
		{"literal action", `{{"abc"}}`, "literal"},
		{"unclosed action", `{{if .x}}a`, "unexpected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			var perr *Error
			if !errors.As(err, &perr) {
				t.Fatalf("Parse(%q) error = %v, want *Error", tc.src, err)
			}
			if !strings.Contains(perr.Msg, tc.want) {
				t.Errorf("Parse(%q) Msg = %q, want substring %q", tc.src, perr.Msg, tc.want)
			}
		})
	}
}

func TestParseListRejects(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{"with", []string{"a", "{{with .x}}b{{end}}"}, "with"},
		{"trim", []string{"{{- .x}}"}, "trim markers"},
		{"nested field", []string{"{{.x.y}}"}, ".x.y"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseList(tc.items)
			var perr *Error
			if !errors.As(err, &perr) {
				t.Fatalf("ParseList(%q) error = %v, want *Error", tc.items, err)
			}
			if !strings.Contains(perr.Msg, tc.want) {
				t.Errorf("ParseList(%q) Msg = %q, want substring %q", tc.items, perr.Msg, tc.want)
			}
		})
	}
}

func TestErrorLocation(t *testing.T) {
	tests := []struct {
		name string
		do   func() error
		want string // full Error()
	}{
		{"string form", func() error { _, err := Parse(`ab {{.x.y}}`); return err }, "1:6: .x.y is not supported: only .x, .env.NAME, .module_root and .module_name are available"},
		{"second line", func() error { _, err := Parse("a\n  {{- .x}}"); return err }, "2:3: trim markers {{- -}} are not supported"},
		{"list item", func() error { _, err := ParseList([]string{"a", "b {{.x.y}}"}); return err }, "[2] 1:5: .x.y is not supported: only .x, .env.NAME, .module_root and .module_name are available"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.do()
			if err == nil || err.Error() != tc.want {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
