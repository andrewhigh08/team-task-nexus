package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type AppError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(code int, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func Wrap(code int, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

var (
	ErrNotFound         = New(http.StatusNotFound, "resource not found")
	ErrUnauthorized     = New(http.StatusUnauthorized, "unauthorized")
	ErrForbidden        = New(http.StatusForbidden, "forbidden")
	ErrBadRequest       = New(http.StatusBadRequest, "bad request")
	ErrConflict         = New(http.StatusConflict, "resource already exists")
	ErrInternal         = New(http.StatusInternalServerError, "internal server error")
	ErrInvalidCredentials = New(http.StatusUnauthorized, "invalid email or password")
	ErrEmailTaken       = New(http.StatusConflict, "email already taken")
	ErrNotTeamMember    = New(http.StatusForbidden, "user is not a member of this team")
	ErrInsufficientRole = New(http.StatusForbidden, "insufficient role for this action")
	ErrRateLimited      = New(http.StatusTooManyRequests, "rate limit exceeded")
)

func BadRequest(msg string) *AppError {
	return New(http.StatusBadRequest, msg)
}

func NotFound(msg string) *AppError {
	return New(http.StatusNotFound, msg)
}

func Internal(msg string, err error) *AppError {
	return Wrap(http.StatusInternalServerError, msg, err)
}

func IsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
