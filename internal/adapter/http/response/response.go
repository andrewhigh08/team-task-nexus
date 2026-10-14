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
	Error   *ErrorBody `json:"error,omitempty"`
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
		body = &ErrorBody{Code: apperror.CodeInternal, Message: internalErrorMessage}
	}
	write(w, status, Envelope{Success: false, Error: body})
}

func write(w http.ResponseWriter, status int, envelope Envelope) {
	body, err := json.Marshal(envelope)
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
