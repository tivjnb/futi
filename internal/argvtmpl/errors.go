package argvtmpl

// Error is a template syntax or unsupported-construct error with its position.
type Error struct {
	// Loc is "line:col" within the template source (both 1-based), or
	// "[N] line:col" for the N-th (1-based) item of a list-form template.
	// Errors reported by the Go parser itself carry only the line. Loc is
	// empty for errors not tied to a position (an unused input).
	Loc string
	// Msg describes the problem. It never contains input or env values.
	Msg string
}

func (e *Error) Error() string {
	if e.Loc == "" {
		return e.Msg
	}
	return e.Loc + ": " + e.Msg
}
