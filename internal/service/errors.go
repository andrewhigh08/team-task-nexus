package service

import "github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"

func errNotTeamMember() error {
	return apperror.Forbidden("user is not a member of this team")
}

// errInvalidCredentials does not say whether the email or the password was wrong, so the
// answer cannot be used to find out which emails are registered.
func errInvalidCredentials() error {
	return apperror.Unauthorized("invalid email or password")
}
