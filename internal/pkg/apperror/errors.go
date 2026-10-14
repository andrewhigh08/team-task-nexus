// Package apperror defines the structured error that crosses layer boundaries: a machine
// readable code, a client-safe message, the HTTP status it maps to and an optional cause that
// is never serialized.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// Error codes sent to clients.
const (
	CodeBadRequest           = "BAD_REQUEST"
	CodeValidation           = "VALIDATION_ERROR"
	CodeUnauthorized         = "UNAUTHORIZED"
	CodeForbidden            = "FORBIDDEN"
	CodeNotFound             = "NOT_FOUND"
	CodeConflict             = "CONFLICT"
	CodeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE"
	CodePayloadTooLarge      = "PAYLOAD_TOO_LARGE"
	CodeTooManyRequests      = "TOO_MANY_REQUESTS"
	CodeInternal             = "INTERNAL_ERROR"
)

// AppError is a structured application error. HTTPStatus and Err are never serialized.
type AppError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	HTTPStatus int            `json:"-"`
	Details    map[string]any `json:"details,omitempty"`
	Err        error          `json:"-"`
}

// Error renders the code, the message and the cause when there is one.
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the cause for errors.Is and errors.As.
func (e *AppError) Unwrap() error {
	return e.Err
}

// BadRequest reports a request the service cannot act on.
func BadRequest(message string) *AppError {
	return &AppError{
		Code:       CodeBadRequest,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
	}
}

// ValidationError reports invalid input, with details naming the offending fields.
func ValidationError(message string, details map[string]any) *AppError {
	return &AppError{
		Code:       CodeValidation,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
		Details:    details,
	}
}

// Unauthorized reports a failed authentication; an empty message means "authentication required".
func Unauthorized(message string) *AppError {
	if message == "" {
		message = "authentication required"
	}
	return &AppError{
		Code:       CodeUnauthorized,
		Message:    message,
		HTTPStatus: http.StatusUnauthorized,
	}
}

// Forbidden reports a denied access; an empty message means "access denied".
func Forbidden(message string) *AppError {
	if message == "" {
		message = "access denied"
	}
	return &AppError{
		Code:       CodeForbidden,
		Message:    message,
		HTTPStatus: http.StatusForbidden,
	}
}

// NotFound reports that the resource with the given id does not exist.
func NotFound(resource string, id any) *AppError {
	return &AppError{
		Code:       CodeNotFound,
		Message:    resource + " not found",
		HTTPStatus: http.StatusNotFound,
		Details: map[string]any{
			"resource": resource,
			"id":       id,
		},
	}
}

// Conflict reports that a resource with the same field value already exists.
func Conflict(resource, field string, value any) *AppError {
	return &AppError{
		Code:       CodeConflict,
		Message:    fmt.Sprintf("%s with this %s already exists", resource, field),
		HTTPStatus: http.StatusConflict,
		Details: map[string]any{
			"resource": resource,
			"field":    field,
			"value":    value,
		},
	}
}

// UnsupportedMediaType reports a request body in a format the endpoint does not accept.
func UnsupportedMediaType(message string) *AppError {
	return &AppError{
		Code:       CodeUnsupportedMediaType,
		Message:    message,
		HTTPStatus: http.StatusUnsupportedMediaType,
	}
}

// PayloadTooLarge reports a request body above the limit of limitBytes.
func PayloadTooLarge(limitBytes int64) *AppError {
	return &AppError{
		Code:       CodePayloadTooLarge,
		Message:    "request body is too large",
		HTTPStatus: http.StatusRequestEntityTooLarge,
		Details: map[string]any{
			"limit_bytes": limitBytes,
		},
	}
}

// TooManyRequests reports an exceeded rate limit with the wait in seconds.
func TooManyRequests(message string, retryAfter int) *AppError {
	return &AppError{
		Code:       CodeTooManyRequests,
		Message:    message,
		HTTPStatus: http.StatusTooManyRequests,
		Details: map[string]any{
			"retry_after_seconds": retryAfter,
		},
	}
}

// Internal reports a server-side failure, wrapping its cause when there is one. The message
// names the failed operation for the log; clients only ever see a generic message.
func Internal(message string, err error) *AppError {
	return &AppError{
		Code:       CodeInternal,
		Message:    message,
		HTTPStatus: http.StatusInternalServerError,
		Err:        err,
	}
}

// AsAppError returns the AppError that err is or wraps, and whether there is one.
func AsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// FromError returns the AppError that err is or wraps, a generic internal error wrapping any
// other error, and nil for a nil error.
func FromError(err error) *AppError {
	if err == nil {
		return nil
	}
	if appErr, ok := AsAppError(err); ok {
		return appErr
	}
	return Internal("internal server error", err)
}
