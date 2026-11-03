package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/termtext"
)

// errNeedsConfirmation marks the refusal to run a destructive command without
// --yes when there is nobody to ask.
var errNeedsConfirmation = errors.New("needs confirmation")

// needsConfirmationError is the refusal text; it unwraps to
// errNeedsConfirmation.
type needsConfirmationError struct{ path string }

func (e *needsConfirmationError) Error() string {
	return fmt.Sprintf("%q is destructive and needs confirmation (no terminal on stdin): rerun as %q",
		e.path, "futi --yes "+strings.TrimPrefix(e.path, "futi ")+" ...")
}

func (e *needsConfirmationError) Unwrap() error { return errNeedsConfirmation }

// newConfirm builds the core.ExecuteRequest.Confirm callback. With yes set it
// approves silently. Without a terminal on stdin it refuses with
// errNeedsConfirmation. Otherwise it prints the command to stderr and reads
// the answer from stdin one byte at a time, so that everything after the
// answer line is left for the child process.
func newConfirm(yes bool, stdin io.Reader, stderr io.Writer, interactive func() bool) func(core.ConfirmRequest) (bool, error) {
	return func(req core.ConfirmRequest) (bool, error) {
		if yes {
			return true, nil
		}
		path := termtext.Line("futi " + strings.Join(req.Path, " "))
		if !interactive() {
			return false, &needsConfirmationError{path: path}
		}
		fmt.Fprintf(stderr, "%s is destructive and will run:\n  %s\nRun it? [y/N] ", path, termtext.Line(shellJoin(req.Argv)))
		answer, err := readAnswer(stdin)
		if err != nil {
			return false, err
		}
		answer = strings.TrimSpace(answer)
		return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
	}
}

// readAnswer reads up to and including the next newline (or EOF), one byte at
// a time, and returns the line without the newline.
func readAnswer(r io.Reader) (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buf[0])
		}
		if err == io.EOF {
			return string(line), nil
		}
		if err != nil {
			return "", err
		}
	}
}
