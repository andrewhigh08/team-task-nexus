package port

// TokenVerifier checks the access tokens presented by clients.
type TokenVerifier interface {
	// Verify returns the id of the user token was issued to. A token that is malformed,
	// forged, expired or issued by someone else fails with an apperror UNAUTHORIZED.
	Verify(token string) (int64, error)
}
