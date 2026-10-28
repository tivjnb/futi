// Package modules implements the module manager behind the futi CLI.
package modules

import (
	"bufio"
	"io"
)

// Prompter asks the user questions on behalf of the manager.
type Prompter interface {
	// Interactive reports whether answers can be read from a terminal.
	Interactive() bool
	// ReadLine reads one answer line with the semantics of
	// bufio.Reader.ReadString('\n') over a single buffer per Prompter.
	ReadLine() (string, error)
}

// Streams are the I/O endpoints of one manager invocation.
type Streams struct {
	Prompt Prompter
	Out    io.Writer // progress, delete prompt, hook stdout, git clone stdout
	Err    io.Writer // warnings, attach/replace prompts, hook/git/curl stderr
	Stdin  io.Reader // stdin of hooks, git and curl
}

type prompter struct {
	in          io.Reader
	br          *bufio.Reader
	interactive func() bool
}

// NewPrompter returns a Prompter reading answers from in. A nil in yields
// io.EOF on every ReadLine; a nil interactive means non-interactive. The
// input is not read until the first ReadLine.
func NewPrompter(in io.Reader, interactive func() bool) Prompter {
	return &prompter{in: in, interactive: interactive}
}

func (p *prompter) Interactive() bool {
	return p.interactive != nil && p.interactive()
}

func (p *prompter) ReadLine() (string, error) {
	if p.in == nil {
		return "", io.EOF
	}
	if p.br == nil {
		p.br = bufio.NewReader(p.in)
	}
	return p.br.ReadString('\n')
}
