package service

import "github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"

func isNotFound(err error) bool {
	appErr, ok := apperror.AsAppError(err)
	return ok && appErr.Code == apperror.CodeNotFound
}

func errNotTeamMember() error {
	return apperror.Forbidden("user is not a member of this team")
}

// errInvalidCredentials does not say whether the email or the password was wrong, so the
// answer cannot be used to find out which emails are registered.
func errInvalidCredentials() error {
	return apperror.Unauthorized("invalid email or password")
}

// errInvalidToken keeps the reason a token was rejected as the cause, for logs and tests; the
// client only learns that the token is not accepted.
func errInvalidToken(cause error) error {
	err := apperror.Unauthorized("invalid or expired token")
	err.Err = cause
	return err
}
