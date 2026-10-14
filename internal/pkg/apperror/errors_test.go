package apperror

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppError_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{name: "without cause", err: NotFound("task", 7), want: "NOT_FOUND: task not found"},
		{
			name: "with cause",
			err:  Internal("create task", errors.New("connection refused")),
			want: "INTERNAL_ERROR: create task: connection refused",
		},
		{name: "internal without cause", err: Internal("create task", nil), want: "INTERNAL_ERROR: create task"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestAppError_Unwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("original error")
	appErr := Internal("wrapped", cause)

	require.ErrorIs(t, appErr, cause)
	require.ErrorIs(t, fmt.Errorf("handler: %w", appErr), cause, "the cause stays reachable through further wrapping")
	assert.NoError(t, BadRequest("no cause").Unwrap())
}

func TestConstructors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		err     *AppError
		code    string
		message string
		status  int
		details map[string]any
	}{
		{name: "BadRequest", err: BadRequest("invalid JSON"), code: CodeBadRequest, message: "invalid JSON", status: http.StatusBadRequest},
		{
			name: "ValidationError", err: ValidationError("invalid input", map[string]any{"title": "must not be empty"}),
			code: CodeValidation, message: "invalid input", status: http.StatusBadRequest,
			details: map[string]any{"title": "must not be empty"},
		},
		{name: "Unauthorized", err: Unauthorized("invalid token"), code: CodeUnauthorized, message: "invalid token", status: http.StatusUnauthorized},
		{name: "Unauthorized default message", err: Unauthorized(""), code: CodeUnauthorized, message: "authentication required", status: http.StatusUnauthorized},
		{name: "Forbidden", err: Forbidden("owner only"), code: CodeForbidden, message: "owner only", status: http.StatusForbidden},
		{name: "Forbidden default message", err: Forbidden(""), code: CodeForbidden, message: "access denied", status: http.StatusForbidden},
		{
			name: "NotFound", err: NotFound("task", int64(42)), code: CodeNotFound, message: "task not found", status: http.StatusNotFound,
			details: map[string]any{"resource": "task", "id": int64(42)},
		},
		{
			name: "Conflict", err: Conflict("user", "email", "a@example.com"), code: CodeConflict,
			message: "user with this email already exists", status: http.StatusConflict,
			details: map[string]any{"resource": "user", "field": "email", "value": "a@example.com"},
		},
		{
			name: "UnsupportedMediaType", err: UnsupportedMediaType("expected application/json"), code: CodeUnsupportedMediaType,
			message: "expected application/json", status: http.StatusUnsupportedMediaType,
		},
		{
			name: "PayloadTooLarge", err: PayloadTooLarge(1 << 20), code: CodePayloadTooLarge,
			message: "request body is too large", status: http.StatusRequestEntityTooLarge,
			details: map[string]any{"limit_bytes": int64(1 << 20)},
		},
		{
			name: "TooManyRequests", err: TooManyRequests("rate limit exceeded", 60), code: CodeTooManyRequests,
			message: "rate limit exceeded", status: http.StatusTooManyRequests, details: map[string]any{"retry_after_seconds": 60},
		},
		{name: "Internal", err: Internal("list tasks", nil), code: CodeInternal, message: "list tasks", status: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.code, tt.err.Code)
			assert.Equal(t, tt.message, tt.err.Message)
			assert.Equal(t, tt.status, tt.err.HTTPStatus)
			assert.Equal(t, tt.details, tt.err.Details)
			assert.NoError(t, tt.err.Err)
		})
	}
}

func TestConstructors_ReturnIndependentValues(t *testing.T) {
	t.Parallel()
	first := Forbidden("")
	first.Message = "changed by a caller"

	assert.Equal(t, "access denied", Forbidden("").Message, "errors are built per call, never shared")
}

func TestAppError_JSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{
			name: "status and cause are never serialized",
			err:  &AppError{Code: CodeNotFound, Message: "task not found", HTTPStatus: http.StatusNotFound, Details: map[string]any{"resource": "task", "id": 1}, Err: errors.New("hidden cause")},
			want: `{"code":"NOT_FOUND","message":"task not found","details":{"resource":"task","id":1}}`,
		},
		{name: "details are omitted when empty", err: BadRequest("bad"), want: `{"code":"BAD_REQUEST","message":"bad"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.err)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(raw))
		})
	}
}

func TestAsAppError(t *testing.T) {
	t.Parallel()
	original := NotFound("team", 1)
	tests := []struct {
		name string
		err  error
		want *AppError
	}{
		{name: "AppError", err: original, want: original},
		{name: "wrapped AppError", err: fmt.Errorf("wrapped: %w", original), want: original},
		{name: "AppError inside a joined error", err: errors.Join(errors.New("other"), original), want: original},
		{name: "plain error", err: errors.New("regular error")},
		{name: "nil error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := AsAppError(tt.err)

			assert.Equal(t, tt.want != nil, ok)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tt.want, got)
		})
	}
}

func TestFromError(t *testing.T) {
	t.Parallel()

	t.Run("nil error", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, FromError(nil))
	})

	t.Run("AppError is returned unchanged", func(t *testing.T) {
		t.Parallel()
		original := NotFound("task", 1)

		assert.Same(t, original, FromError(original))
	})

	t.Run("wrapped AppError is unwrapped", func(t *testing.T) {
		t.Parallel()
		original := Unauthorized("token expired")

		assert.Same(t, original, FromError(fmt.Errorf("auth failed: %w", original)))
	})

	t.Run("plain error becomes an internal error", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("something went wrong")

		result := FromError(cause)

		require.NotNil(t, result)
		assert.Equal(t, CodeInternal, result.Code)
		assert.Equal(t, "internal server error", result.Message)
		assert.Equal(t, http.StatusInternalServerError, result.HTTPStatus)
		assert.ErrorIs(t, result, cause)
	})
}
