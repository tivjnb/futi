package modules

import (
	"io"
	"strings"
	"testing"
)

func TestPrompterSharesOneBuffer(t *testing.T) {
	p := NewPrompter(strings.NewReader("y\nno\n"), nil)
	for _, want := range []string{"y\n", "no\n"} {
		if got, err := p.ReadLine(); got != want || err != nil {
			t.Fatalf("ReadLine() = %q, %v; want %q, nil", got, err, want)
		}
	}
	if got, err := p.ReadLine(); got != "" || err != io.EOF {
		t.Fatalf("ReadLine() at end = %q, %v; want \"\", io.EOF", got, err)
	}
}

func TestPrompterNilReader(t *testing.T) {
	p := NewPrompter(nil, nil)
	if got, err := p.ReadLine(); got != "" || err != io.EOF {
		t.Fatalf("ReadLine() = %q, %v; want \"\", io.EOF", got, err)
	}
	if p.Interactive() {
		t.Fatal("Interactive() = true for nil interactive func, want false")
	}
}

func TestPrompterInteractive(t *testing.T) {
	for _, want := range []bool{true, false} {
		p := NewPrompter(nil, func() bool { return want })
		if got := p.Interactive(); got != want {
			t.Fatalf("Interactive() = %v, want %v", got, want)
		}
	}
}

type failingReader struct{ t *testing.T }

func (r failingReader) Read([]byte) (int, error) {
	r.t.Error("Read called before ReadLine")
	return 0, io.EOF
}

func TestPrompterReadsLazily(t *testing.T) {
	p := NewPrompter(failingReader{t}, nil)
	_ = p.Interactive()
}

func TestPrompterPartialLastLine(t *testing.T) {
	p := NewPrompter(strings.NewReader("y"), nil)
	if got, err := p.ReadLine(); got != "y" || err != io.EOF {
		t.Fatalf("ReadLine() = %q, %v; want \"y\", io.EOF", got, err)
	}
}
