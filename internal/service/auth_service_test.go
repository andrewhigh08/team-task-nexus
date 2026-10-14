package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

const (
	testSecret = "0123456789abcdef0123456789abcdef"
	testIssuer = "team-task-nexus"
	testTTL    = time.Hour
	// testBcryptCost differs from every cost bcrypt falls back to, so the tests can tell that
	// the configured one is used.
	testBcryptCost       = bcrypt.MinCost + 1
	testUserID     int64 = 42
	testEmail            = "alice@example.com"
	testPassword         = "correct horse battery"
	testFullName         = "Alice Example"
)

// testNow is the frozen clock of the service under test. It lies in the past, so a token
// accepted by Verify proves that the service reads time from its clock.
var testNow = time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)

func testAuthConfig() AuthConfig {
	return AuthConfig{
		Secret:     []byte(testSecret),
		Issuer:     testIssuer,
		TTL:        testTTL,
		BcryptCost: testBcryptCost,
		Clock:      func() time.Time { return testNow },
	}
}

// authFixture is an AuthService on a gomock user repository that records the hashes logins
// are checked against.
type authFixture struct {
	svc      *AuthService
	users    *mocks.MockUserRepository
	compared [][]byte
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	users := mocks.NewMockUserRepository(gomock.NewController(t))
	svc, err := NewAuthService(users, testAuthConfig())
	require.NoError(t, err)
	f := &authFixture{svc: svc, users: users}
	svc.compare = func(hash, password []byte) error {
		f.compared = append(f.compared, hash)
		return bcrypt.CompareHashAndPassword(hash, password)
	}
	return f
}

// storedUser returns the user the repository holds for testEmail, whose password is testPassword.
func storedUser(t *testing.T) *domain.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	require.NoError(t, err)
	return &domain.User{
		ID:           testUserID,
		Email:        testEmail,
		PasswordHash: string(hash),
		FullName:     testFullName,
		CreatedAt:    testNow.Add(-24 * time.Hour),
		UpdatedAt:    testNow.Add(-time.Hour),
	}
}

func validClaims() jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		ID:        "00112233445566778899aabbccddeeff",
		Issuer:    testIssuer,
		Subject:   "42",
		IssuedAt:  jwt.NewNumericDate(testNow),
		ExpiresAt: jwt.NewNumericDate(testNow.Add(testTTL)),
	}
}

func signToken(t *testing.T, method jwt.SigningMethod, key any, claims jwt.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	require.NoError(t, err)
	return token
}

// parseClaims decodes a token issued by the service under test, independently of Verify.
func parseClaims(t *testing.T, token string) *jwt.RegisteredClaims {
	t.Helper()
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(*jwt.Token) (any, error) { return []byte(testSecret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return testNow }))
	require.NoError(t, err)
	return claims
}

func requireFieldErrors(t *testing.T, err error, want map[string]string) {
	t.Helper()
	appErr := assertAppError(t, err, apperror.CodeValidation)
	assert.Equal(t, map[string]any{"fields": want}, appErr.Details)
}

func TestNewAuthService_RejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*AuthConfig)
		want   string
	}{
		{name: "missing secret", mutate: func(c *AuthConfig) { c.Secret = nil }, want: "signing secret is required"},
		{name: "missing issuer", mutate: func(c *AuthConfig) { c.Issuer = "" }, want: "issuer is required"},
		{name: "zero TTL", mutate: func(c *AuthConfig) { c.TTL = 0 }, want: "token TTL must be positive"},
		{name: "bcrypt cost below range", mutate: func(c *AuthConfig) { c.BcryptCost = bcrypt.MinCost - 1 }, want: "bcrypt cost must be between 4 and 31, got 3"},
		{name: "bcrypt cost above range", mutate: func(c *AuthConfig) { c.BcryptCost = bcrypt.MaxCost + 1 }, want: "got 32"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := testAuthConfig()
			tt.mutate(&cfg)

			svc, err := NewAuthService(mocks.NewMockUserRepository(gomock.NewController(t)), cfg)

			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, svc)
		})
	}
}

func TestNewAuthService_HashesTheDummyPasswordAtTheConfiguredCost(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)

	cost, err := bcrypt.Cost(f.svc.dummyHash)
	require.NoError(t, err)
	assert.Equal(t, testBcryptCost, cost, "an unknown email costs the same bcrypt work as a registered one")
}

func TestNewAuthService_DefaultsToTheWallClock(t *testing.T) {
	t.Parallel()
	cfg := testAuthConfig()
	cfg.Clock = nil
	svc, err := NewAuthService(mocks.NewMockUserRepository(gomock.NewController(t)), cfg)
	require.NoError(t, err)

	res, err := svc.authResult(&domain.User{ID: testUserID})

	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(testTTL), res.ExpiresAt, 2*time.Second)
	userID, err := svc.Verify(res.Token)
	require.NoError(t, err)
	assert.Equal(t, testUserID, userID)
}

func TestAuthService_Register_StoresTheNormalizedUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		in           port.RegisterInput
		wantEmail    string
		wantFullName string
	}{
		{
			name:      "plain input",
			in:        port.RegisterInput{Email: testEmail, Password: testPassword, FullName: testFullName},
			wantEmail: testEmail, wantFullName: testFullName,
		},
		{
			name:      "email is trimmed and lower-cased, name is trimmed",
			in:        port.RegisterInput{Email: "  Alice@Example.COM\t", Password: testPassword, FullName: "  Alice Example \n"},
			wantEmail: testEmail, wantFullName: testFullName,
		},
		{
			name:      "longest email",
			in:        port.RegisterInput{Email: strings.Repeat("a", 242) + "@example.com", Password: testPassword, FullName: testFullName},
			wantEmail: strings.Repeat("a", 242) + "@example.com", wantFullName: testFullName,
		},
		{
			name:      "international email",
			in:        port.RegisterInput{Email: "Élan@Exämple.com", Password: testPassword, FullName: testFullName},
			wantEmail: "élan@exämple.com", wantFullName: testFullName,
		},
		{
			name:      "shortest password",
			in:        port.RegisterInput{Email: testEmail, Password: "12345678", FullName: testFullName},
			wantEmail: testEmail, wantFullName: testFullName,
		},
		{
			name:      "longest password counted in bytes",
			in:        port.RegisterInput{Email: testEmail, Password: strings.Repeat("я", 36), FullName: testFullName},
			wantEmail: testEmail, wantFullName: testFullName,
		},
		{
			name:      "longest name counted in characters",
			in:        port.RegisterInput{Email: testEmail, Password: testPassword, FullName: strings.Repeat("é", 255)},
			wantEmail: testEmail, wantFullName: strings.Repeat("é", 255),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			stored := &domain.User{ID: testUserID, Email: tt.wantEmail, FullName: tt.wantFullName, CreatedAt: testNow}
			create := f.users.EXPECT().Create(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, u *domain.User) (int64, error) {
					assert.Zero(t, u.ID)
					assert.Equal(t, tt.wantEmail, u.Email)
					assert.Equal(t, tt.wantFullName, u.FullName)
					require.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(tt.in.Password)))
					cost, err := bcrypt.Cost([]byte(u.PasswordHash))
					require.NoError(t, err)
					assert.Equal(t, testBcryptCost, cost)
					stored.PasswordHash = u.PasswordHash
					return testUserID, nil
				})
			f.users.EXPECT().GetByID(gomock.Any(), testUserID).Return(stored, nil).After(create)

			res, err := f.svc.Register(t.Context(), tt.in)

			require.NoError(t, err)
			assert.Equal(t, *stored, res.User, "the result carries the row as the database stored it")
		})
	}
}

func TestAuthService_Register_IssuesAToken(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	f.users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(testUserID, nil)
	f.users.EXPECT().GetByID(gomock.Any(), testUserID).Return(storedUser(t), nil)

	res, err := f.svc.Register(t.Context(), port.RegisterInput{Email: testEmail, Password: testPassword, FullName: testFullName})

	require.NoError(t, err)
	assert.Equal(t, testNow.Add(testTTL), res.ExpiresAt)
	claims := parseClaims(t, res.Token)
	assert.Equal(t, "42", claims.Subject)
	assert.Equal(t, testIssuer, claims.Issuer)
	assert.Equal(t, testNow, claims.IssuedAt.UTC())
	assert.Equal(t, testNow.Add(testTTL), claims.ExpiresAt.UTC())
	assert.Len(t, claims.ID, 2*jtiBytes)
	_, err = hex.DecodeString(claims.ID)
	require.NoError(t, err, "jti is random hex")
}

func TestAuthService_Register_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	valid := port.RegisterInput{Email: testEmail, Password: testPassword, FullName: testFullName}
	tests := []struct {
		name   string
		mutate func(*port.RegisterInput)
		want   map[string]string
	}{
		{name: "missing email", mutate: func(in *port.RegisterInput) { in.Email = "" }, want: map[string]string{"email": "is required"}},
		{name: "blank email", mutate: func(in *port.RegisterInput) { in.Email = "  " }, want: map[string]string{"email": "is required"}},
		{
			name:   "email without a domain",
			mutate: func(in *port.RegisterInput) { in.Email = "alice" },
			want:   map[string]string{"email": "must be a valid email address"},
		},
		{
			name:   "email with a display name",
			mutate: func(in *port.RegisterInput) { in.Email = "Alice <alice@example.com>" },
			want:   map[string]string{"email": "must be a valid email address"},
		},
		{
			name:   "two emails",
			mutate: func(in *port.RegisterInput) { in.Email = "alice@example.com, bob@example.com" },
			want:   map[string]string{"email": "must be a valid email address"},
		},
		{
			name:   "email too long",
			mutate: func(in *port.RegisterInput) { in.Email = strings.Repeat("a", 243) + "@example.com" },
			want:   map[string]string{"email": "must be at most 254 characters"},
		},
		{
			name:   "password too short",
			mutate: func(in *port.RegisterInput) { in.Password = "1234567" },
			want:   map[string]string{"password": "must be 8 to 72 bytes long"},
		},
		{
			name:   "password too long",
			mutate: func(in *port.RegisterInput) { in.Password = strings.Repeat("я", 36) + "!" },
			want:   map[string]string{"password": "must be 8 to 72 bytes long"},
		},
		{name: "missing name", mutate: func(in *port.RegisterInput) { in.FullName = "" }, want: map[string]string{"full_name": "is required"}},
		{name: "blank name", mutate: func(in *port.RegisterInput) { in.FullName = " \t " }, want: map[string]string{"full_name": "is required"}},
		{
			name:   "name too long",
			mutate: func(in *port.RegisterInput) { in.FullName = strings.Repeat("é", 256) },
			want:   map[string]string{"full_name": "must be at most 255 characters"},
		},
		{
			name:   "every field invalid",
			mutate: func(in *port.RegisterInput) { *in = port.RegisterInput{Email: "alice", Password: "short"} },
			want: map[string]string{
				"email":     "must be a valid email address",
				"password":  "must be 8 to 72 bytes long",
				"full_name": "is required",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t) // no expectations: invalid input never reaches the repository
			in := valid
			tt.mutate(&in)

			res, err := f.svc.Register(t.Context(), in)

			assert.Nil(t, res)
			requireFieldErrors(t, err, tt.want)
		})
	}
}

func TestAuthService_Register_ReportsRepositoryFailures(t *testing.T) {
	t.Parallel()
	errDB := errors.New("connection reset")
	tests := []struct {
		name    string
		repoErr *apperror.AppError
		setup   func(users *mocks.MockUserRepository, repoErr error)
	}{
		{
			name:    "email already registered",
			repoErr: apperror.Conflict("user", "email", testEmail),
			setup: func(users *mocks.MockUserRepository, repoErr error) {
				users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(int64(0), repoErr)
			},
		},
		{
			name:    "insert fails",
			repoErr: apperror.Internal("create user", errDB),
			setup: func(users *mocks.MockUserRepository, repoErr error) {
				users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(int64(0), repoErr)
			},
		},
		{
			name:    "reading the new user back fails",
			repoErr: apperror.Internal("get user by id", errDB),
			setup: func(users *mocks.MockUserRepository, repoErr error) {
				users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(testUserID, nil)
				users.EXPECT().GetByID(gomock.Any(), testUserID).Return(nil, repoErr)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			tt.setup(f.users, tt.repoErr)

			res, err := f.svc.Register(t.Context(), port.RegisterInput{Email: testEmail, Password: testPassword, FullName: testFullName})

			assert.Nil(t, res)
			assert.Same(t, tt.repoErr, err, "repository errors are already application errors and pass through unchanged")
		})
	}
}

func TestAuthService_Login_IssuesAToken(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	user := storedUser(t)
	f.users.EXPECT().GetByEmail(gomock.Any(), testEmail).Return(user, nil)

	res, err := f.svc.Login(t.Context(), port.LoginInput{Email: "  ALICE@example.com ", Password: testPassword})

	require.NoError(t, err)
	assert.Equal(t, *user, res.User)
	assert.Equal(t, testNow.Add(testTTL), res.ExpiresAt)
	assert.Equal(t, "42", parseClaims(t, res.Token).Subject)
	assert.Equal(t, [][]byte{[]byte(user.PasswordHash)}, f.compared)
}

func TestAuthService_Login_RejectsInvalidCredentials(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		email      string
		password   string
		registered bool
	}{
		{name: "wrong password", email: testEmail, password: "wrong password", registered: true},
		{name: "unknown email", email: "bob@example.com", password: testPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			user := storedUser(t)
			wantCompared := f.svc.dummyHash
			if tt.registered {
				f.users.EXPECT().GetByEmail(gomock.Any(), tt.email).Return(user, nil)
				wantCompared = []byte(user.PasswordHash)
			} else {
				f.users.EXPECT().GetByEmail(gomock.Any(), tt.email).Return(nil, apperror.NotFound("user", tt.email))
			}

			res, err := f.svc.Login(t.Context(), port.LoginInput{Email: tt.email, Password: tt.password})

			assert.Nil(t, res)
			appErr := assertAppError(t, err, apperror.CodeUnauthorized)
			assert.Equal(t, "invalid email or password", appErr.Message, "both cases get the same answer")
			assert.Equal(t, [][]byte{wantCompared}, f.compared, "bcrypt runs exactly once either way")
		})
	}
}

func TestAuthService_Login_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   port.LoginInput
		want map[string]string
	}{
		{name: "missing email", in: port.LoginInput{Password: testPassword}, want: map[string]string{"email": "is required"}},
		{name: "blank email", in: port.LoginInput{Email: " ", Password: testPassword}, want: map[string]string{"email": "is required"}},
		{name: "missing password", in: port.LoginInput{Email: testEmail}, want: map[string]string{"password": "is required"}},
		{
			name: "password longer than bcrypt reads",
			in:   port.LoginInput{Email: testEmail, Password: strings.Repeat("x", 73)},
			want: map[string]string{"password": "must be at most 72 bytes long"},
		},
		{
			name: "both missing",
			want: map[string]string{"email": "is required", "password": "is required"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)

			res, err := f.svc.Login(t.Context(), tt.in)

			assert.Nil(t, res)
			requireFieldErrors(t, err, tt.want)
			assert.Empty(t, f.compared)
		})
	}
}

func TestAuthService_Login_DoesNotMaskServerFailuresAsBadCredentials(t *testing.T) {
	t.Parallel()
	errDB := errors.New("connection reset")
	tests := []struct {
		name    string
		user    func(t *testing.T) *domain.User
		repoErr error
		// wantMessage is the operation the error names: the repository's own for its failures,
		// which pass through unchanged.
		wantMessage string
		wantErr     error
	}{
		{name: "repository failure", repoErr: apperror.Internal("get user by email", errDB), wantMessage: "get user by email", wantErr: errDB},
		{
			name: "corrupt stored hash",
			user: func(t *testing.T) *domain.User {
				u := storedUser(t)
				u.PasswordHash = "not a bcrypt hash"
				return u
			},
			wantMessage: "compare password hash",
			wantErr:     bcrypt.ErrHashTooShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			var user *domain.User
			if tt.user != nil {
				user = tt.user(t)
			}
			f.users.EXPECT().GetByEmail(gomock.Any(), testEmail).Return(user, tt.repoErr)

			res, err := f.svc.Login(t.Context(), port.LoginInput{Email: testEmail, Password: testPassword})

			assert.Nil(t, res)
			appErr := assertAppError(t, err, apperror.CodeInternal)
			assert.Equal(t, tt.wantMessage, appErr.Message)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestAuthService_Verify_AcceptsTrustedTokens(t *testing.T) {
	t.Parallel()
	issued, err := newAuthFixture(t).svc.authResult(&domain.User{ID: testUserID})
	require.NoError(t, err)
	tests := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{name: "issued by the service", token: func(*testing.T) string { return issued.Token }},
		{
			name: "issued a little ahead by a replica whose clock runs fast",
			token: func(t *testing.T) string {
				claims := validClaims()
				claims.IssuedAt = jwt.NewNumericDate(testNow.Add(clockSkewLeeway - time.Second))
				return signToken(t, jwt.SigningMethodHS256, []byte(testSecret), claims)
			},
		},
		{
			name: "expired a moment ago on a replica whose clock runs slow",
			token: func(t *testing.T) string {
				claims := validClaims()
				claims.IssuedAt = jwt.NewNumericDate(testNow.Add(-testTTL))
				claims.ExpiresAt = jwt.NewNumericDate(testNow.Add(-clockSkewLeeway + time.Second))
				return signToken(t, jwt.SigningMethodHS256, []byte(testSecret), claims)
			},
		},
		{
			name: "without a jti",
			token: func(t *testing.T) string {
				claims := validClaims()
				claims.ID = ""
				return signToken(t, jwt.SigningMethodHS256, []byte(testSecret), claims)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userID, err := newAuthFixture(t).svc.Verify(tt.token(t))

			require.NoError(t, err)
			assert.Equal(t, testUserID, userID)
		})
	}
}

func TestAuthService_Verify_RejectsUntrustedTokens(t *testing.T) {
	t.Parallel()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	secret := []byte(testSecret)
	withClaims := func(mutate func(*jwt.RegisteredClaims)) jwt.RegisteredClaims {
		claims := validClaims()
		mutate(&claims)
		return claims
	}
	tests := []struct {
		name      string
		token     string
		wantCause error
	}{
		{
			name:      "signed with another secret",
			token:     signToken(t, jwt.SigningMethodHS256, []byte(strings.Repeat("x", 32)), validClaims()),
			wantCause: jwt.ErrTokenSignatureInvalid,
		},
		{
			name:      "HS512 with the right secret",
			token:     signToken(t, jwt.SigningMethodHS512, secret, validClaims()),
			wantCause: jwt.ErrTokenSignatureInvalid,
		},
		{
			name:      "RS256",
			token:     signToken(t, jwt.SigningMethodRS256, rsaKey, validClaims()),
			wantCause: jwt.ErrTokenSignatureInvalid,
		},
		{
			name:      "unsigned",
			token:     signToken(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, validClaims()),
			wantCause: jwt.ErrTokenSignatureInvalid,
		},
		{
			name: "expired beyond the leeway",
			token: signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) {
				c.IssuedAt = jwt.NewNumericDate(testNow.Add(-testTTL))
				c.ExpiresAt = jwt.NewNumericDate(testNow.Add(-clockSkewLeeway - time.Second))
			})),
			wantCause: jwt.ErrTokenExpired,
		},
		{
			name: "issued in the future beyond the leeway",
			token: signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) {
				c.IssuedAt = jwt.NewNumericDate(testNow.Add(clockSkewLeeway + time.Second))
			})),
			wantCause: jwt.ErrTokenUsedBeforeIssued,
		},
		{
			name:      "without an expiry",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil })),
			wantCause: jwt.ErrTokenRequiredClaimMissing,
		},
		{
			name:      "issued by another service",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.Issuer = "other-service" })),
			wantCause: jwt.ErrTokenInvalidIssuer,
		},
		{
			name:      "without an issuer",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.Issuer = "" })),
			wantCause: jwt.ErrTokenRequiredClaimMissing,
		},
		{
			name:      "without a subject",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.Subject = "" })),
			wantCause: jwt.ErrTokenInvalidSubject,
		},
		{
			name:      "subject that is not a number",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.Subject = "alice" })),
			wantCause: jwt.ErrTokenInvalidSubject,
		},
		{
			name:      "zero subject",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.Subject = "0" })),
			wantCause: jwt.ErrTokenInvalidSubject,
		},
		{
			name:      "negative subject",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.Subject = "-42" })),
			wantCause: jwt.ErrTokenInvalidSubject,
		},
		{
			name:      "subject beyond int64",
			token:     signToken(t, jwt.SigningMethodHS256, secret, withClaims(func(c *jwt.RegisteredClaims) { c.Subject = "9223372036854775808" })),
			wantCause: jwt.ErrTokenInvalidSubject,
		},
		{name: "not a JWT", token: "not-a-token", wantCause: jwt.ErrTokenMalformed},
		{name: "empty", token: "", wantCause: jwt.ErrTokenMalformed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userID, err := newAuthFixture(t).svc.Verify(tt.token)

			assert.Zero(t, userID)
			appErr := assertAppError(t, err, apperror.CodeUnauthorized)
			assert.Equal(t, "invalid or expired token", appErr.Message)
			require.ErrorIs(t, err, tt.wantCause)
		})
	}
}
