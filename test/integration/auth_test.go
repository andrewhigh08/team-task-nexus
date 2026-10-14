package integration

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAndLogin(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	email := uniqueEmail(t)

	// The address is stored trimmed and in lower case.
	resp := c.register(t, "  "+strings.ToUpper(email)+" ", password, "  Grace Hopper ")
	registered := expect[authData](t, resp, http.StatusCreated)
	assert.Equal(t, "Bearer", registered.TokenType)
	assert.InDelta(t, tokenTTL.Seconds(), registered.ExpiresIn, 1)
	assert.Positive(t, registered.User.ID)
	assert.Equal(t, email, registered.User.Email)
	assert.Equal(t, "Grace Hopper", registered.User.FullName)
	assert.WithinDuration(t, time.Now(), registered.User.CreatedAt, time.Minute)
	assert.Equal(t, time.UTC, registered.User.CreatedAt.Location())

	claims := parseClaims(t, registered.AccessToken)
	assert.Equal(t, strconv.FormatInt(registered.User.ID, 10), claims.Subject)
	assert.Equal(t, tokenIssuer, claims.Issuer)
	assert.Len(t, claims.ID, 32)

	// The token opens the API; a new user has no teams, which is [] and not null.
	teams := c.do(t, http.MethodGet, "/api/v1/teams", nil, registered.AccessToken)
	requireStatus(t, teams, http.StatusOK)
	assert.JSONEq(t, `[]`, string(teams.Data))

	loggedIn := expect[authData](t, c.login(t, strings.ToUpper(email), password), http.StatusOK)
	assert.Equal(t, registered.User, loggedIn.User)
	assert.NotEqual(t, registered.AccessToken, loggedIn.AccessToken, "every token has its own id")
	requireStatus(t, c.do(t, http.MethodGet, "/api/v1/teams", nil, loggedIn.AccessToken), http.StatusOK)

	duplicate := requireError(t, c.register(t, strings.ToUpper(email), password, "Impostor"), http.StatusConflict, "CONFLICT")
	assert.Equal(t, "user with this email already exists", duplicate.Message)
}

// TestLookalikeEmailsAreDistinctAccounts covers addresses that a collation ignoring accents,
// character width or ß against ss would take for one address.
func TestLookalikeEmailsAreDistinctAccounts(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Lookalike Owner")

	tests := []struct {
		name      string
		plain     string
		lookalike string
	}{
		{name: "accent", plain: "rene", lookalike: "rené"},
		{name: "sharp s", plain: "strasse", lookalike: "straße"},
		{name: "full-width letter", plain: "alice", lookalike: "ａlice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			suffix := "-" + uniqueEmail(t)
			plain := expect[authData](t, c.register(t, tc.plain+suffix, password, "Plain"), http.StatusCreated)
			lookalike := expect[authData](t, c.register(t, tc.lookalike+suffix, password, "Lookalike"), http.StatusCreated)
			assert.NotEqual(t, plain.User.ID, lookalike.User.ID)
			assert.Equal(t, tc.lookalike+suffix, lookalike.User.Email)

			// Each address logs in to its own account.
			for _, registered := range []authData{plain, lookalike} {
				loggedIn := expect[authData](t, c.login(t, registered.User.Email, password), http.StatusOK)
				assert.Equal(t, registered.User, loggedIn.User)
			}

			// Adding the lookalike address to a team adds its account, not the plain one.
			team := c.mustCreateTeam(t, owner, "Lookalikes "+tc.name)
			member := expect[memberData](t, c.addMember(t, owner, team.ID, lookalike.User.Email, ""), http.StatusCreated)
			assert.Equal(t, lookalike.User.ID, member.UserID)
		})
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	t.Parallel()
	c := newClient(t)

	tests := []struct {
		name     string
		email    string
		password string
		fullName string
		problems map[string]string
	}{
		{
			name:     "malformed email",
			email:    "not-an-email",
			password: password,
			fullName: "Ada",
			problems: map[string]string{"email": "must be a valid email address"},
		},
		{
			name:     "email with a display name",
			email:    "Ada <ada@example.com>",
			password: password,
			fullName: "Ada",
			problems: map[string]string{"email": "must be a valid email address"},
		},
		{
			name:     "short password",
			email:    uniqueEmail(t),
			password: "short",
			fullName: "Ada",
			problems: map[string]string{"password": "must be 8 to 72 bytes long"},
		},
		{
			name:     "password longer than bcrypt hashes",
			email:    uniqueEmail(t),
			password: strings.Repeat("p", 73),
			fullName: "Ada",
			problems: map[string]string{"password": "must be 8 to 72 bytes long"},
		},
		{
			name:     "every field at once",
			email:    "",
			password: "",
			fullName: "   ",
			problems: map[string]string{
				"email":     "is required",
				"password":  "must be 8 to 72 bytes long",
				"full_name": "is required",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errBody := requireError(t, c.register(t, tc.email, tc.password, tc.fullName), http.StatusBadRequest, "VALIDATION_ERROR")
			assert.Equal(t, map[string]any{"fields": toAny(tc.problems)}, errBody.Details)
		})
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	acc := c.mustRegister(t, "Alan Turing")

	tests := []struct {
		name    string
		email   string
		pass    string
		status  int
		code    string
		message string
		field   string
	}{
		{
			name: "wrong password", email: acc.Email, pass: "wrong-password",
			status: http.StatusUnauthorized, code: "UNAUTHORIZED", message: "invalid email or password",
		},
		{
			name: "unknown email gets the same answer", email: "unknown-" + acc.Email, pass: password,
			status: http.StatusUnauthorized, code: "UNAUTHORIZED", message: "invalid email or password",
		},
		{
			name: "missing password", email: acc.Email, pass: "",
			status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: "invalid input", field: "password",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errBody := requireError(t, c.login(t, tc.email, tc.pass), tc.status, tc.code)
			assert.Equal(t, tc.message, errBody.Message)
			if tc.field != "" {
				assert.Equal(t, "is required", fieldProblem(errBody, tc.field))
			}
		})
	}
}

func TestRequestBodiesAreChecked(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	valid := `{"email":"` + uniqueEmail(t) + `","password":"` + password + `","full_name":"Body Check"}`

	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        string
	}{
		{name: "unknown field", body: `{"email":"a@example.com","password":"x","admin":true}`, status: http.StatusBadRequest, code: "BAD_REQUEST"},
		{name: "malformed JSON", body: `{"email":`, status: http.StatusBadRequest, code: "BAD_REQUEST"},
		{name: "two JSON values", body: valid + valid, status: http.StatusBadRequest, code: "BAD_REQUEST"},
		{name: "not declared as JSON", contentType: "text/plain", body: valid, status: http.StatusUnsupportedMediaType, code: "UNSUPPORTED_MEDIA_TYPE"},
		{name: "larger than 1 MiB", body: `{"full_name":"` + strings.Repeat("a", 1<<20) + `"}`, status: http.StatusRequestEntityTooLarge, code: "PAYLOAD_TOO_LARGE"},
	}
	// Sequential, so that the registration below runs after every rejected attempt.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := c.newRequest(t, http.MethodPost, "/api/v1/register", rawBody(tc.body), "")
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			requireError(t, c.send(t, req), tc.status, tc.code)
		})
	}

	// None of the rejected bodies holding the valid one registered the account.
	expect[authData](t, c.do(t, http.MethodPost, "/api/v1/register", rawBody(valid), ""), http.StatusCreated)
}

func TestProtectedRoutesRequireAValidToken(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	acc := c.mustRegister(t, "Token Holder")
	claims := parseClaims(t, acc.Token)

	expired := claims
	expired.IssuedAt = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	foreign := claims
	foreign.Issuer = "someone-else"

	tests := []struct {
		name    string
		header  string
		message string
	}{
		{name: "missing header", message: "authentication required"},
		{name: "wrong scheme", header: "Basic " + acc.Token, message: "invalid authorization header"},
		{name: "bearer without token", header: "Bearer", message: "invalid authorization header"},
		{name: "garbage token", header: "Bearer not.a.jwt", message: "invalid or expired token"},
		{name: "signed with another secret", header: "Bearer " + sign(t, claims, "another-secret-of-at-least-32-bytes!"), message: "invalid or expired token"},
		{name: "expired", header: "Bearer " + sign(t, expired, jwtSecret), message: "invalid or expired token"},
		{name: "foreign issuer", header: "Bearer " + sign(t, foreign, jwtSecret), message: "invalid or expired token"},
		{name: "unsigned", header: "Bearer " + unsigned(t, claims), message: "invalid or expired token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := c.newRequest(t, http.MethodGet, "/api/v1/teams", nil, "")
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			resp := c.send(t, req)
			errBody := requireError(t, resp, http.StatusUnauthorized, "UNAUTHORIZED")
			assert.Equal(t, tc.message, errBody.Message)
			assert.Equal(t, "Bearer", resp.Header.Get("WWW-Authenticate"))
		})
	}

	// The scheme is case-insensitive.
	req := c.newRequest(t, http.MethodGet, "/api/v1/teams", nil, "")
	req.Header.Set("Authorization", "bearer "+acc.Token)
	requireStatus(t, c.send(t, req), http.StatusOK)
}

// parseClaims decodes the claims of a token without verifying it.
func parseClaims(t *testing.T, token string) jwt.RegisteredClaims {
	t.Helper()
	var claims jwt.RegisteredClaims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	require.NoError(t, err)
	return claims
}

func sign(t *testing.T, claims jwt.RegisteredClaims, secret string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

func unsigned(t *testing.T, claims jwt.RegisteredClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	return token
}

// toAny converts m to the shape encoding/json decodes an object into.
func toAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
