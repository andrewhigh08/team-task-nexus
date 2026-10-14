package middleware

import "net/http"

// responseRecorder passes a response through and remembers its status and size, for the
// access log, the metrics and the panic recovery.
type responseRecorder struct {
	http.ResponseWriter
	status int // 0 until the header has been written
	bytes  int
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w}
}

func (rec *responseRecorder) WriteHeader(status int) {
	if rec.status == 0 {
		rec.status = status
	}
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *responseRecorder) Write(p []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	n, err := rec.ResponseWriter.Write(p)
	rec.bytes += n
	return n, err
}

// Flush flushes the wrapped writer when it can, so that wrapping a writer does not take
// streaming away from the handlers below.
func (rec *responseRecorder) Flush() {
	flusher, ok := rec.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	flusher.Flush()
}

// Unwrap returns the wrapped writer, where http.ResponseController looks for the features
// the recorder does not pass on itself.
func (rec *responseRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

// written reports whether the response header has been sent.
func (rec *responseRecorder) written() bool {
	return rec.status != 0
}

// finalStatus returns the status the client receives once the handler is done. completed is
// false when the handler panicked: a handler that returns without writing gets 200 from
// net/http, one that panics before writing gets 500 from Recoverer.
func (rec *responseRecorder) finalStatus(completed bool) int {
	switch {
	case rec.written():
		return rec.status
	case completed:
		return http.StatusOK
	default:
		return http.StatusInternalServerError
	}
}
