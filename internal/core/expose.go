package core

import (
	"fmt"
	"strings"

	"github.com/tivjnb/futi/internal/execmodule"
)

// NotExposedError reports a command that exists but is not exposed to the
// frontend it was asked from. It is not classified (ErrNotFound/ErrInvalidInput).
type NotExposedError struct {
	Path     []string // path words: "deploy", "prod"
	Frontend execmodule.Frontend
	Expose   []execmodule.Frontend // frontends the command is exposed to
}

func (e *NotExposedError) Error() string {
	expose := make([]string, len(e.Expose))
	for i, f := range e.Expose {
		expose[i] = string(f)
	}
	return fmt.Sprintf("command %q is not available in %s (expose: %s)",
		strings.Join(append([]string{"futi"}, e.Path...), " "), frontendLabel(e.Frontend), strings.Join(expose, ", "))
}

// frontendLabel is the name of a frontend in messages.
func frontendLabel(f execmodule.Frontend) string {
	switch f {
	case execmodule.FrontendCLI:
		return "the CLI"
	case execmodule.FrontendMCP:
		return "MCP"
	case execmodule.FrontendHTTP:
		return "HTTP"
	}
	return string(f)
}

// notExposed returns a *NotExposedError if cmd at path is hidden from f.
func notExposed(f execmodule.Frontend, path []string, cmd *execmodule.Command) error {
	if cmd.ExposedTo(f) {
		return nil
	}
	return &NotExposedError{Path: append([]string(nil), path...), Frontend: f, Expose: cmd.Expose}
}
