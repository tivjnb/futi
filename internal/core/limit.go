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
// the rest: truncation by the limit is not reported to the child. A failure
// or a short write of the underlying writer is returned as (k, err) or
// (k, io.ErrShortWrite).
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
		k, err := lw.w.Write(p[:n])
		lw.l.left -= int64(k)
		if err != nil {
			return k, err
		}
		if int64(k) < n {
			return k, io.ErrShortWrite
		}
	}
	return len(p), nil
}

// outputWriters prepares the child's output writers. With limit == 0 the
// writers pass through and the limiter is nil. Otherwise the non-nil streams
// share one budget; if stdout and stderr are the same writer, they get the
// same wrapper. A nil stream stays nil (the executor sends it to the null
// device without a pipe) and does not consume the budget.
func outputWriters(stdout, stderr io.Writer, limit int64) (o, e io.Writer, l *outputLimiter) {
	if limit <= 0 {
		return stdout, stderr, nil
	}
	if stdout == nil && stderr == nil {
		return nil, nil, nil
	}
	l = &outputLimiter{left: limit}
	if stdout != nil {
		o = &limitedWriter{l: l, w: stdout}
	}
	if stderr != nil {
		if o != nil && sameWriter(stdout, stderr) {
			return o, o, l
		}
		e = &limitedWriter{l: l, w: stderr}
	}
	return o, e, l
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
