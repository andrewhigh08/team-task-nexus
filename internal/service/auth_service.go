package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

const (
	// clockSkewLeeway is the clock difference tolerated between the replica that issued a token
	// and the one verifying it.
	clockSkewLeeway = 30 * time.Second
	// jtiBytes is the number of random bytes in the jti claim.
	jtiBytes = 16
)

// AuthConfig configures token signing and password hashing. A nil Clock means time.Now.
type AuthConfig struct {
	Secret     []byte
	Issuer     string
	TTL        time.Duration
	BcryptCost int
	Clock      func() time.Time
}

func (c AuthConfig) validate() error {
	switch {
	case len(c.Secret) == 0:
		return errors.New("auth service: signing secret is required")
	case c.Issuer == "":
		return errors.New("auth service: issuer is required")
	case c.TTL <= 0:
		return errors.New("auth service: token TTL must be positive")
	case c.BcryptCost < bcrypt.MinCost || c.BcryptCost > bcrypt.MaxCost:
		return fmt.Errorf("auth service: bcrypt cost must be between %d and %d, got %d",
			bcrypt.MinCost, bcrypt.MaxCost, c.BcryptCost)
	}
	return nil
}

// AuthService registers and logs in users with HS256 access tokens and verifies those tokens.
// It implements port.AuthService and port.TokenVerifier. Tokens are not checked against the
// user store, so they cannot be revoked; their lifetime is bounded by the configured TTL.
type AuthService struct {
	users  port.UserRepository
	cfg    AuthConfig
	parser *jwt.Parser
	// dummyHash is a hash of a random password at the configured cost. Logins with an unknown
	// email are checked against it, so they cost the same bcrypt work as logins with a
	// registered one.
	dummyHash []byte
	// compare is bcrypt.CompareHashAndPassword; tests replace it to observe which hash a login
	// was checked against.
	compare func(hash, password []byte) error
}

// NewAuthService validates cfg and prepares the hash that unknown emails are checked against.
func NewAuthService(users port.UserRepository, cfg AuthConfig) (*AuthService, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	dummyHash, err := bcrypt.GenerateFromPassword([]byte(rand.Text()), cfg.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth service: hash dummy password: %w", err)
	}
	return &AuthService{
		users: users,
		cfg:   cfg,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(cfg.Issuer),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithLeeway(clockSkewLeeway),
			jwt.WithTimeFunc(cfg.Clock),
		),
		dummyHash: dummyHash,
		compare:   bcrypt.CompareHashAndPassword,
	}, nil
}

// Register validates in, stores the user with a normalized email and a bcrypt hash of the
// password, and issues a token for the new account.
func (s *AuthService) Register(ctx context.Context, in port.RegisterInput) (*port.AuthResult, error) {
	email := normalizeEmail(in.Email)
	fullName := strings.TrimSpace(in.FullName)
	errs := fieldErrors{}
	errs.checkEmail("email", email)
	errs.checkNewPassword("password", in.Password)
	errs.checkRequiredText("full_name", fullName, maxFullNameChars)
	if err := errs.err(); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), s.cfg.BcryptCost)
	if err != nil {
		return nil, apperror.Internal("hash password", err)
	}
	id, err := s.users.Create(ctx, &domain.User{Email: email, PasswordHash: string(hash), FullName: fullName})
	if err != nil {
		return nil, err
	}
	// The row is read back for the timestamps the database assigned.
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.Internal("load registered user", err)
	}
	return s.authResult(user)
}

// Login issues a token when the password matches the account with the given email.
func (s *AuthService) Login(ctx context.Context, in port.LoginInput) (*port.AuthResult, error) {
	email := normalizeEmail(in.Email)
	if err := validateLogin(email, in.Password); err != nil {
		return nil, err
	}

	user, err := s.users.GetByEmail(ctx, email)
	if isNotFound(err) {
		// Spend the same bcrypt work as for a registered email, so the response time does not
		// reveal which emails are registered.
		_ = s.compare(s.dummyHash, []byte(in.Password))
		return nil, errInvalidCredentials()
	}
	if err != nil {
		return nil, apperror.Internal("load user by email", err)
	}

	err = s.compare([]byte(user.PasswordHash), []byte(in.Password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return nil, errInvalidCredentials()
	}
	if err != nil {
		return nil, apperror.Internal("compare password hash", err)
	}
	return s.authResult(user)
}

// validateLogin only checks what a registered password can never violate; length rules for
// new passwords may change without locking existing accounts out.
func validateLogin(email, password string) error {
	errs := fieldErrors{}
	if email == "" {
		errs.add("email", msgRequired)
	}
	switch {
	case password == "":
		errs.add("password", msgRequired)
	case len(password) > maxPasswordBytes:
		errs.add("password", fmt.Sprintf("must be at most %d bytes long", maxPasswordBytes))
	}
	return errs.err()
}

// Verify checks the signature, algorithm, issuer, expiry and issue time of token, tolerating
// clockSkewLeeway, and returns the user id in its subject.
func (s *AuthService) Verify(token string) (int64, error) {
	claims := &jwt.RegisteredClaims{}
	if _, err := s.parser.ParseWithClaims(token, claims, s.signingKey); err != nil {
		return 0, errInvalidToken(err)
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return 0, errInvalidToken(jwt.ErrTokenInvalidSubject)
	}
	return userID, nil
}

// signingKey is the jwt.Keyfunc of Verify. The parser has already rejected every algorithm
// but HS256, so the shared secret is the only key to offer.
func (s *AuthService) signingKey(*jwt.Token) (any, error) {
	return s.cfg.Secret, nil
}

// authResult issues an access token for user. JWT timestamps have whole-second precision, so
// the clock is truncated to keep the returned expiry equal to the exp claim.
func (s *AuthService) authResult(user *domain.User) (*port.AuthResult, error) {
	now := s.cfg.Clock().Truncate(time.Second)
	expiresAt := now.Add(s.cfg.TTL)
	claims := jwt.RegisteredClaims{
		ID:        newTokenID(),
		Issuer:    s.cfg.Issuer,
		Subject:   strconv.FormatInt(user.ID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.cfg.Secret)
	if err != nil {
		return nil, apperror.Internal("sign access token", err)
	}
	return &port.AuthResult{Token: token, ExpiresAt: expiresAt, User: *user}, nil
}

// newTokenID returns a random jti, which lets a token be told apart from another issued to
// the same user in the same second.
func newTokenID() string {
	buf := make([]byte, jtiBytes)
	// crypto/rand.Read never returns an error; it crashes the program instead.
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
