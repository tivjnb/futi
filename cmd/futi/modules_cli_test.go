package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCLIStreamsWiring(t *testing.T) {
	var out bytes.Buffer
	setStdinTerminal(t, true)
	s := cliStreams(&out, strings.NewReader("y\n"))
	if s.Out != io.Writer(&out) || s.Err != io.Writer(os.Stderr) || s.Stdin != io.Reader(os.Stdin) {
		t.Fatalf("cliStreams wiring: Out=%v Err=%v Stdin=%v", s.Out, s.Err, s.Stdin)
	}
	if !s.Prompt.Interactive() {
		t.Fatal("Interactive() must follow stdinIsTerminal")
	}
	if line, _ := s.Prompt.ReadLine(); line != "y\n" {
		t.Fatalf("ReadLine() = %q, want answers from stdin argument", line)
	}
}
