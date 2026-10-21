package core

import (
	"io"
	"sync"
)

// outputLimiter is the shared byte budget of the command's stdout and stderr.
type outputLimiter struct {
	mu        sync.Mutex
	left      int64
	truncated bool
}

// Truncated reports whether any output was dropped because of the limit.
func (l *outputLimiter) Truncated() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.truncated
}

// limitedWriter writes to w while the shared budget lasts and silently drops
// the rest. Write always reports success, so the child never sees a failure.
type limitedWriter struct {
	l *outputLimiter
	w io.Writer
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	lw.l.mu.Lock()
	defer lw.l.mu.Unlock()
	n := int64(len(p))
	if n > lw.l.left {
		n = lw.l.left
		lw.l.truncated = true
	}
	if n > 0 {
		if _, err := lw.w.Write(p[:n]); err != nil {
			return 0, err
		}
		lw.l.left -= n
	}
	return len(p), nil
}

// outputWriters prepares the child's output writers. nil becomes io.Discard.
// With limit == 0 the writers pass through and the limiter is nil. Otherwise
// both streams share one budget; if stdout and stderr are the same writer,
// they get the same wrapper.
func outputWriters(stdout, stderr io.Writer, limit int64) (o, e io.Writer, l *outputLimiter) {
	if limit <= 0 {
		return stdout, stderr, nil
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	l = &outputLimiter{left: limit}
	o = &limitedWriter{l: l, w: stdout}
	if sameWriter(stdout, stderr) {
		return o, o, l
	}
	return o, &limitedWriter{l: l, w: stderr}, l
}

// sameWriter reports whether a and b are the same writer. Comparing
// interfaces holding an uncomparable dynamic type panics; such writers are
// treated as different.
func sameWriter(a, b io.Writer) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}
