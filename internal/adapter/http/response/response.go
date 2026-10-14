// Package response defines the JSON envelope shared by every API endpoint and the helpers
// that write it.
package response

import (
	"encoding/json"
	"net/http"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// internalErrorMessage is the only message a client ever sees for an internal error.
const internalErrorMessage = "internal server error"

// internalErrorBody is sent when a payload cannot be encoded, a programming error that
// leaves nothing better to say.
var internalErrorBody = []byte(`{"success":false,"error":{"code":"` + apperror.CodeInternal +
	`","message":"` + internalErrorMessage + `"}}` + "\n")

// Envelope is the body of every API response.
type Envelope struct {
	Success bool       `json:"success"`
	Data    any        `json:"data,omitempty"`
	Meta    *Meta      `json:"meta,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

// Meta describes the page of a paged list: Data holds page number Page of TotalPages pages
// of up to PageSize items, out of Total items.
type Meta struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// ErrorBody describes why a request failed.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// JSON writes data inside a success envelope with the given status.
func JSON(w http.ResponseWriter, status int, data any) {
	write(w, status, Envelope{Success: true, Data: data})
}

// JSONWithMeta writes one page of a paged list inside a success envelope with status 200.
func JSONWithMeta(w http.ResponseWriter, data any, meta Meta) {
	write(w, http.StatusOK, Envelope{Success: true, Data: data, Meta: &meta})
}

// Error writes err inside an error envelope with the status of its AppError; any other error
// becomes a 500 INTERNAL_ERROR. Server errors are logged with their cause through the request
// logger, and an internal error is always answered with a generic message: its own message
// names the failed operation for the log, not for the client. Causes are never sent.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	appErr := apperror.FromError(err)
	if appErr == nil {
		appErr = apperror.Internal(internalErrorMessage, nil)
	}
	status := appErr.HTTPStatus
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusInternalServerError
	}
	if status >= http.StatusInternalServerError {
		logger.FromContext(r.Context()).Error("request failed", "status", status, "code", appErr.Code, "error", err)
	}

	body := &ErrorBody{Code: appErr.Code, Message: appErr.Message, Details: appErr.Details}
	if appErr.Code == apperror.CodeInternal {
		body = internalError()
	}
	write(w, status, Envelope{Success: false, Error: body})
}

// InternalError writes the generic 500 INTERNAL_ERROR envelope. Unlike Error it logs nothing:
// it serves callers that have already logged the failure in more detail, such as the panic
// recovery.
func InternalError(w http.ResponseWriter) {
	write(w, http.StatusInternalServerError, Envelope{Success: false, Error: internalError()})
}

func internalError() *ErrorBody {
	return &ErrorBody{Code: apperror.CodeInternal, Message: internalErrorMessage}
}

// Plain writes v as JSON with the given status, outside the envelope. Only the health probes
// use it: their bodies follow the conventions of probes rather than those of the API.
func Plain(w http.ResponseWriter, status int, v any) {
	write(w, status, v)
}

func write(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status, body = http.StatusInternalServerError, internalErrorBody
	} else {
		body = append(body, '\n')
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write means the client has gone; there is no one left to tell.
	_, _ = w.Write(body)
}
