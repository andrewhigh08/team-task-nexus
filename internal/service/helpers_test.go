package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// assertAppError asserts that err is or wraps an AppError with the given code.
func assertAppError(t *testing.T, err error, code string) {
	t.Helper()
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "%v is not an AppError", err)
	assert.Equal(t, code, appErr.Code)
}
