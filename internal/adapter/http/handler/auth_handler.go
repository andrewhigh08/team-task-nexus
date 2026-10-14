package handler

import (
	"net/http"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// tokenType is the OAuth 2.0 token type of the issued access tokens (RFC 6750).
const tokenType = "Bearer"

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresIn   int64        `json:"expires_in"`
	User        userResponse `json:"user"`
}

type userResponse struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	CreatedAt time.Time `json:"created_at"`
}

// newAuthResponse maps res to the response body. expires_in is counted from now, in whole
// seconds as RFC 6749 defines it.
func newAuthResponse(res *port.AuthResult) authResponse {
	return authResponse{
		AccessToken: res.Token,
		TokenType:   tokenType,
		ExpiresIn:   int64(time.Until(res.ExpiresAt).Round(time.Second) / time.Second),
		User:        newUserResponse(res.User),
	}
}

func newUserResponse(u domain.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, FullName: u.FullName, CreatedAt: u.CreatedAt.UTC()}
}

// AuthHandler serves registration and login.
type AuthHandler struct {
	authSvc port.AuthService
}

// NewAuthHandler returns an AuthHandler backed by authSvc.
func NewAuthHandler(authSvc port.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

// Register creates an account and answers 201 with a token for it.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	res, err := h.authSvc.Register(r.Context(), port.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusCreated, newAuthResponse(res))
}

// Login answers 200 with a token for valid credentials.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	res, err := h.authSvc.Login(r.Context(), port.LoginInput{Email: req.Email, Password: req.Password})
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, newAuthResponse(res))
}
