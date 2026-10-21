package core

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
)

func TestOutputWritersNoLimitPassesThrough(t *testing.T) {
	o, e, l := outputWriters(os.Stdout, os.Stderr, 0)
	if o != os.Stdout || e != os.Stderr {
		t.Errorf("writers = %v, %v; want os.Stdout, os.Stderr unchanged", o, e)
	}
	if l != nil {
		t.Errorf("limiter = %v, want nil for limit 0", l)
	}
}

func TestOutputWritersSharedWhenSame(t *testing.T) {
	var buf bytes.Buffer
	o, e, l := outputWriters(&buf, &buf, 10)
	if o != e {
		t.Errorf("same destination must share one limited writer, got %p and %p", o, e)
	}
	if l == nil {
		t.Fatal("limiter is nil for limit 10")
	}
}

type funcWriter func([]byte) (int, error)

func (f funcWriter) Write(p []byte) (int, error) { return f(p) }

func TestOutputWritersUncomparable(t *testing.T) {
	w := funcWriter(func(p []byte) (int, error) { return len(p), nil })
	// Comparing interfaces holding a func panics; must be handled.
	o, e, _ := outputWriters(w, w, 10)
	if o == e {
		t.Error("uncomparable writers must not be treated as the same")
	}
}

type sliceWriter []byte

func (sliceWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestSameWriter(t *testing.T) {
	var a, b bytes.Buffer
	cases := []struct {
		name string
		x, y io.Writer
		want bool
	}{
		{"same pointer", &a, &a, true},
		{"different pointers", &a, &b, false},
		{"uncomparable slice type", sliceWriter{}, sliceWriter{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameWriter(tc.x, tc.y); got != tc.want {
				t.Errorf("sameWriter = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLimitedWriterTruncates(t *testing.T) {
	var buf bytes.Buffer
	o, _, l := outputWriters(&buf, nil, 5)
	n, err := o.Write([]byte("hello world"))
	if n != 11 || err != nil {
		t.Errorf("Write = %d, %v; want 11, nil", n, err)
	}
	if got := buf.String(); got != "hello" {
		t.Errorf("written = %q, want %q", got, "hello")
	}
	if !l.Truncated() {
		t.Error("Truncated() = false, want true")
	}
}

func TestLimitedWriterWithinLimit(t *testing.T) {
	var buf bytes.Buffer
	o, _, l := outputWriters(&buf, nil, 5)
	if _, err := o.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if l.Truncated() {
		t.Error("Truncated() = true for 3 bytes under limit 5")
	}
}

func TestOutputWritersNilStreamsStayNilWithLimit(t *testing.T) {
	var buf bytes.Buffer
	cases := []struct {
		name         string
		stdout, errw io.Writer
		wantONil     bool
		wantENil     bool
	}{
		{"both nil", nil, nil, true, true},
		{"stdout nil", nil, &buf, true, false},
		{"stderr nil", &buf, nil, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, e, _ := outputWriters(tc.stdout, tc.errw, 10)
			if (o == nil) != tc.wantONil || (e == nil) != tc.wantENil {
				t.Errorf("writers nil = (%v, %v), want (%v, %v)", o == nil, e == nil, tc.wantONil, tc.wantENil)
			}
		})
	}
}

func TestLimitedWriterNilStreamDoesNotConsumeBudget(t *testing.T) {
	var buf bytes.Buffer
	o, e, l := outputWriters(&buf, nil, 5)
	if e != nil {
		t.Fatalf("stderr writer = %v, want nil", e)
	}
	if _, err := o.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if l.Truncated() || buf.String() != "hello" {
		t.Errorf("truncated=%v written=%q, want false, %q", l.Truncated(), buf.String(), "hello")
	}
}

func TestLimitedWriterShortWrite(t *testing.T) {
	short := funcWriter(func(p []byte) (int, error) { return len(p) - 1, nil })
	o, _, l := outputWriters(short, nil, 100)
	n, err := o.Write([]byte("abcd"))
	if !errors.Is(err, io.ErrShortWrite) || n != 3 {
		t.Errorf("Write = %d, %v; want 3, io.ErrShortWrite", n, err)
	}
	if l.left != 97 {
		t.Errorf("left = %d, want 97 (only written bytes consumed)", l.left)
	}
}
