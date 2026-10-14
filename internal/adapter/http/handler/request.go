package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const msgPositive = "must be a positive integer"

// jsonMediaType is the media type request bodies must be sent in.
const jsonMediaType = "application/json"

// decodeJSON decodes the JSON request body into dst and reports whether it could. When it
// could not, the error response has been written: 415 for a body that is not declared as
// application/json, 413 for one over the body limit and 400 for a malformed one. Unknown
// fields and anything after the JSON value count as malformed, so that a misspelled field is
// reported instead of silently ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := readJSON(r, dst); err != nil {
		response.Error(w, r, err)
		return false
	}
	return true
}

func readJSON(r *http.Request, dst any) error {
	if r.ContentLength != 0 && !isJSON(r.Header.Get("Content-Type")) {
		return apperror.UnsupportedMediaType("request body must be " + jsonMediaType)
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return bodyError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return bodyError(err)
	}
	return nil
}

// isJSON reports whether contentType declares JSON; parameters such as charset do not matter.
func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == jsonMediaType
}

// bodyError maps a failure to decode the body: a body over the limit is too large, anything
// else is malformed.
func bodyError(cause error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(cause, &tooLarge) {
		return apperror.PayloadTooLarge(tooLarge.Limit)
	}
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

// pageQuery reads the paging query parameters page and page_size. Absent ones are zero, which
// the services replace with the first page and the default size.
func pageQuery(r *http.Request) (domain.PageRequest, error) {
	page, err := queryInt(r, "page")
	if err != nil {
		return domain.PageRequest{}, err
	}
	pageSize, err := queryInt(r, "page_size")
	if err != nil {
		return domain.PageRequest{}, err
	}
	return domain.PageRequest{Page: page, PageSize: pageSize}, nil
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
