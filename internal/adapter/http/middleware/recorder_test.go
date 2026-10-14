package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseRecorder_FinalStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		write     func(w http.ResponseWriter)
		completed bool
		want      int
	}{
		{name: "explicit status", write: func(w http.ResponseWriter) { w.WriteHeader(http.StatusCreated) }, completed: true, want: http.StatusCreated},
		{name: "first status wins", write: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusNotFound)
			w.WriteHeader(http.StatusOK)
		}, completed: true, want: http.StatusNotFound},
		{name: "body without a status", write: func(w http.ResponseWriter) { _, _ = w.Write([]byte("ok")) }, completed: true, want: http.StatusOK},
		{name: "nothing written", write: func(http.ResponseWriter) {}, completed: true, want: http.StatusOK},
		{name: "panic before writing", write: func(http.ResponseWriter) {}, want: http.StatusInternalServerError},
		{name: "panic after writing", write: func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) }, want: http.StatusAccepted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := newResponseRecorder(httptest.NewRecorder())

			tt.write(rec)

			assert.Equal(t, tt.want, rec.finalStatus(tt.completed))
		})
	}
}

func TestResponseRecorder_PassesTheResponseOn(t *testing.T) {
	t.Parallel()
	inner := httptest.NewRecorder()
	rec := newResponseRecorder(inner)

	rec.WriteHeader(http.StatusAccepted)
	n, err := rec.Write([]byte("hello, "))
	require.NoError(t, err)
	_, err = rec.Write([]byte("world"))
	require.NoError(t, err)

	assert.Equal(t, 7, n)
	assert.Equal(t, http.StatusAccepted, inner.Code)
	assert.Equal(t, "hello, world", inner.Body.String())
	assert.Equal(t, 12, rec.bytes)
	assert.True(t, rec.written())
}

func TestResponseRecorder_Flush(t *testing.T) {
	t.Parallel()

	t.Run("flushes a flushable writer", func(t *testing.T) {
		t.Parallel()
		inner := httptest.NewRecorder()
		rec := newResponseRecorder(inner)

		http.Flusher(rec).Flush()

		assert.True(t, inner.Flushed)
		assert.Equal(t, http.StatusOK, rec.finalStatus(false), "flushing sends the header")
	})

	t.Run("ignores a writer that cannot flush", func(t *testing.T) {
		t.Parallel()
		rec := newResponseRecorder(struct{ http.ResponseWriter }{httptest.NewRecorder()})

		rec.Flush()

		assert.False(t, rec.written())
	})
}

// deadlineWriter is a writer that supports write deadlines, which the recorder does not pass
// on itself.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func TestResponseRecorder_Unwrap(t *testing.T) {
	t.Parallel()
	inner := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	rec := newResponseRecorder(inner)
	deadline := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	err := http.NewResponseController(rec).SetWriteDeadline(deadline)

	require.NoError(t, err, "http.ResponseController reaches the wrapped writer")
	assert.Equal(t, deadline, inner.deadline)
}
