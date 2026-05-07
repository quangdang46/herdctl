package pipeline

import (
	"bytes"
	"io"
)

// cappedWriter is a bounded io.Writer backed by an in-memory buffer. Writes
// past the cap are silently dropped from the buffer but the total observed
// byte count is preserved on Truncated/Total so callers can report accurate
// truncation diagnostics. Used by executeCommand to bound stdout and stderr
// during command execution rather than after-the-fact, so stderr-heavy
// commands cannot consume unbounded memory before cmd.Wait() returns
// (bd-g7cu9).
//
// cappedWriter is NOT safe for concurrent Write calls. exec.Cmd serialises
// writes per stream, so this is fine for the executor's single goroutine
// per stdout/stderr usage. Mixing two streams into one cappedWriter is also
// safe because exec.Cmd internally serialises calls when stdout and stderr
// share the same writer.
type cappedWriter struct {
	buf       bytes.Buffer
	cap       int64
	truncated bool
	total     int64 // total bytes observed across all Write calls
}

// newCappedWriter returns a writer that drops bytes once cap is reached.
// A non-positive cap disables truncation (unbounded buffer).
func newCappedWriter(cap int64) *cappedWriter {
	return &cappedWriter{cap: cap}
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	if w.cap <= 0 {
		w.buf.Write(p)
		return len(p), nil
	}
	remaining := w.cap - int64(w.buf.Len())
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

// Bytes returns the captured (possibly truncated) payload.
func (w *cappedWriter) Bytes() []byte { return w.buf.Bytes() }

// String is the convenience accessor used by callers that immediately
// stringify the captured output.
func (w *cappedWriter) String() string { return w.buf.String() }

// Len returns the size of the captured payload (post-truncation).
func (w *cappedWriter) Len() int { return w.buf.Len() }

// Total returns the total number of bytes observed by Write across all
// calls, including bytes that were dropped after the cap.
func (w *cappedWriter) Total() int64 { return w.total }

// Truncated reports whether at least one byte was dropped due to the cap.
func (w *cappedWriter) Truncated() bool { return w.truncated }

// ensureCappedWriterIsWriter is a compile-time assertion.
var _ io.Writer = (*cappedWriter)(nil)
