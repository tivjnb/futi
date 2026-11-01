package execmodule

import (
	"errors"
	"testing"
)

func TestTemplateErrorMessage(t *testing.T) {
	err := &TemplateError{Cmd: "echo {{.x", Cause: errors.New("boom")}
	if got, want := err.Error(), `failed to render template "echo {{.x": boom`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
