package main

import (
	"io"
	"os"

	"github.com/tivjnb/futi/internal/modules"
)

// cliStreams wires the manager to the process streams. w receives progress
// output; stdin supplies answers to prompts. The Interactive check is a
// closure so tests can replace stdinIsTerminal.
func cliStreams(w io.Writer, stdin io.Reader) modules.Streams {
	return modules.Streams{
		Prompt: modules.NewPrompter(stdin, func() bool { return stdinIsTerminal() }),
		Out:    w,
		Err:    os.Stderr,
		Stdin:  os.Stdin,
	}
}
