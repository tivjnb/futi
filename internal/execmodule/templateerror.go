package execmodule

import "fmt"

// TemplateError is a template parse or execution error. It carries the
// template and the underlying cause; the CLI formats it in a friendly way,
// hiding the chain of wrappers and the text/template trace.
type TemplateError struct {
	Cmd   string
	Cause error
}

// Error returns a short message with the template and the cause.
func (e *TemplateError) Error() string {
	return fmt.Sprintf("failed to render template %q: %v", e.Cmd, e.Cause)
}

// Unwrap returns the underlying cause (for errors.As/Is).
func (e *TemplateError) Unwrap() error { return e.Cause }
