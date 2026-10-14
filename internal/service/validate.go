package service

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// Input limits. Text limits count characters of the trimmed value. Password limits count bytes:
// bcrypt hashes only the first 72 bytes, so a longer password would match on its prefix alone.
const (
	maxEmailChars           = 254
	minPasswordBytes        = 8
	maxPasswordBytes        = 72
	maxFullNameChars        = 255
	maxTeamNameChars        = 255
	maxTeamDescriptionChars = 5000
	maxTaskTitleChars       = 255
	maxTaskDescriptionChars = 10000
	maxCommentChars         = 5000
)

const (
	msgRequired = "is required"
	msgNotNull  = "must not be null"
	msgPositive = "must be a positive integer"
)

// fieldErrors maps each invalid input field to what is wrong with it. Services check every
// field before failing, so that a client learns about all problems in one round trip.
type fieldErrors map[string]string

// add records problem for field unless an earlier check already rejected the field.
func (e fieldErrors) add(field, problem string) {
	if _, ok := e[field]; !ok {
		e[field] = problem
	}
}

// err returns the collected problems as a VALIDATION_ERROR whose details name every invalid
// field, or nil when there are none.
func (e fieldErrors) err() error {
	if len(e) == 0 {
		return nil
	}
	return apperror.ValidationError("invalid input", map[string]any{"fields": map[string]string(e)})
}

// checkEmail expects email to be normalized already. net/mail also accepts forms such as
// "Alice <alice@example.com>"; requiring the parsed address to equal the input admits only
// the bare address.
func (e fieldErrors) checkEmail(field, email string) {
	switch {
	case email == "":
		e.add(field, msgRequired)
	case utf8.RuneCountInString(email) > maxEmailChars:
		e.add(field, fmt.Sprintf("must be at most %d characters", maxEmailChars))
	default:
		if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
			e.add(field, "must be a valid email address")
		}
	}
}

// checkNewPassword checks a password that is about to be hashed.
func (e fieldErrors) checkNewPassword(field, password string) {
	if n := len(password); n < minPasswordBytes || n > maxPasswordBytes {
		e.add(field, fmt.Sprintf("must be %d to %d bytes long", minPasswordBytes, maxPasswordBytes))
	}
}

// checkRequiredText expects value to be trimmed already and requires 1 to maxChars characters.
func (e fieldErrors) checkRequiredText(field, value string, maxChars int) {
	switch {
	case value == "":
		e.add(field, msgRequired)
	case utf8.RuneCountInString(value) > maxChars:
		e.add(field, fmt.Sprintf("must be at most %d characters", maxChars))
	}
}

// checkOptionalText expects value to be trimmed already and allows up to maxChars characters.
func (e fieldErrors) checkOptionalText(field, value string, maxChars int) {
	if utf8.RuneCountInString(value) > maxChars {
		e.add(field, fmt.Sprintf("must be at most %d characters", maxChars))
	}
}

// checkNotNull rejects a field of a partial update that was set to null although it cannot
// be cleared.
func (e fieldErrors) checkNotNull(field string, null bool) {
	if null {
		e.add(field, msgNotNull)
	}
}

func (e fieldErrors) checkPositive(field string, id int64) {
	if id <= 0 {
		e.add(field, msgPositive)
	}
}

func (e fieldErrors) checkStatus(field string, status domain.TaskStatus) {
	if !status.Valid() {
		e.add(field, "must be one of todo, in_progress, review, done")
	}
}

func (e fieldErrors) checkPriority(field string, priority domain.TaskPriority) {
	if !priority.Valid() {
		e.add(field, "must be 1 (low), 2 (medium) or 3 (high)")
	}
}

// normalizeEmail returns the form emails are stored and looked up in.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
