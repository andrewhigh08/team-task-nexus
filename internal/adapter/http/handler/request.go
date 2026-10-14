package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const msgPositive = "must be a positive integer"

// decodeJSON decodes the request body into dst. Unknown fields and anything after the JSON
// value are rejected, so that a misspelled field is reported instead of silently ignored.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errInvalidBody(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errInvalidBody(err)
	}
	return nil
}

func errInvalidBody(cause error) error {
	err := apperror.BadRequest("invalid request body")
	err.Err = cause
	return err
}

// pathID returns the URL parameter name as a positive id.
func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, invalidField(name, msgPositive)
	}
	return id, nil
}

// queryID returns the query parameter name as a positive id, or 0 when it is absent.
func queryID(r *http.Request, name string) (int64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, invalidField(name, msgPositive)
	}
	return id, nil
}

// queryInt returns the query parameter name as a positive integer, or 0 when it is absent.
func queryInt(r *http.Request, name string) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, invalidField(name, msgPositive)
	}
	return n, nil
}

// parseDate parses a calendar date in YYYY-MM-DD form as midnight UTC.
func parseDate(field, value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, invalidField(field, "must be a date in YYYY-MM-DD format")
	}
	return date, nil
}

// invalidField reports a malformed request field in the shape the services use for invalid
// input, so clients handle both alike.
func invalidField(field, problem string) error {
	return apperror.ValidationError("invalid input", map[string]any{"fields": map[string]string{field: problem}})
}

// mapAll maps items with fn. The result is never nil, so an empty list is encoded as [] and
// not as null.
func mapAll[T, U any](items []T, fn func(T) U) []U {
	out := make([]U, 0, len(items))
	for _, item := range items {
		out = append(out, fn(item))
	}
	return out
}
