package execmodule

import (
	"strings"
	"testing"
	"text/template"
)

func TestHookQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", `'plain'`},
		{"a b", `'a b'`},
		{"", `''`},
		{"it's", `'it'\''s'`},
		{`$HOME "x"`, `'$HOME "x"'`},
	}
	tmpl := template.Must(template.New("h").Funcs(TemplateFuncs()).Parse(`{{quote .}}`))
	for _, tt := range tests {
		var b strings.Builder
		if err := tmpl.Execute(&b, tt.in); err != nil {
			t.Fatal(err)
		}
		if b.String() != tt.want {
			t.Errorf("quote(%q) = %q, want %q", tt.in, b.String(), tt.want)
		}
	}
}

func TestTemplateErrorMessage(t *testing.T) {
	err := &TemplateError{Cmd: "echo {{.x", Cause: errTest}
	if got, want := err.Error(), `failed to render template "echo {{.x": boom`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

var errTest = testErr("boom")

type testErr string

func (e testErr) Error() string { return string(e) }
