package api

import (
	"context"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewAuthTestServer returns a Server with session-based OIDC auth enabled but without a live OIDC
// provider. It exercises the auth guard, the config endpoint, and session cookies (which need only
// the session secret); the provider-backed login/callback flow is not covered here.
func NewAuthTestServer(kubeClient client.Client, secret []byte) *Server {
	return &Server{
		Service: NewCRClusterService(kubeClient),
		OIDC:    OIDCConfig{IssuerURL: "https://issuer.test", SessionSecret: secret},
		auth:    &authenticator{config: OIDCConfig{SessionSecret: secret, SessionTTL: time.Hour}},
	}
}

// NewSessionCookie returns a valid signed session cookie for the given subject.
func (s *Server) NewSessionCookie(subject string) *http.Cookie {
	return s.auth.signedCookie(
		sessionCookieName,
		mustMarshal(sessionClaims{Subject: subject, Expiry: time.Now().Add(time.Hour).Unix()}),
		"/",
		int(time.Hour.Seconds()),
	)
}

// Test seam for black-box (api_test) coverage of the OIDC authenticator internals.

// Authenticator aliases the unexported authenticator type for use in black-box tests.
type Authenticator = authenticator

// SessionClaims aliases the authenticated identity persisted in the session cookie.
type SessionClaims = sessionClaims

// StateData aliases the CSRF state and OIDC nonce persisted in the state cookie.
type StateData = stateData

// Cookie names and the login path used by the OIDC flow, re-exported for tests.
const (
	SessionCookieName = sessionCookieName
	StateCookieName   = stateCookieName
	LoginPath         = loginPath
)

// ErrInvalidCookie is the sentinel returned by VerifySignedValue for a malformed/tampered cookie.
var ErrInvalidCookie = errInvalidCookie

// SignValue HMAC-signs a payload, exposing the unexported helper to black-box tests.
func SignValue(secret, payload []byte) string { return signValue(secret, payload) }

// VerifySignedValue verifies and returns a signed cookie payload (or ErrInvalidCookie).
func VerifySignedValue(secret []byte, value string) ([]byte, error) {
	return verifySignedValue(secret, value)
}

// MustMarshal JSON-encodes a value, exposing the unexported helper to black-box tests.
func MustMarshal(value any) []byte { return mustMarshal(value) }

// NewAuthenticator discovers the OIDC provider and builds an authenticator (network I/O).
func NewAuthenticator(ctx context.Context, cfg OIDCConfig) (*Authenticator, error) {
	return newAuthenticator(ctx, cfg)
}

// NewConfigAuthenticator builds an authenticator from config alone (no OIDC discovery), for tests
// that exercise cookie/session/callback behavior without a live provider.
func NewConfigAuthenticator(cfg OIDCConfig) *Authenticator {
	return &authenticator{config: cfg}
}

// SignedCookie exposes the unexported cookie-signing method to black-box tests.
func (a *authenticator) SignedCookie(
	name string,
	payload []byte,
	path string,
	maxAge int,
) *http.Cookie {
	return a.signedCookie(name, payload, path, maxAge)
}

// CurrentUser exposes the unexported session-resolution method to black-box tests.
func (a *authenticator) CurrentUser(request *http.Request) (SessionClaims, bool) {
	return a.currentUser(request)
}

// HandleLogin exposes the unexported login handler to black-box tests.
func (a *authenticator) HandleLogin(writer http.ResponseWriter, request *http.Request) {
	a.handleLogin(writer, request)
}

// HandleCallback exposes the unexported callback handler to black-box tests.
func (a *authenticator) HandleCallback(writer http.ResponseWriter, request *http.Request) {
	a.handleCallback(writer, request)
}

// HandleLogout exposes the unexported logout handler to black-box tests.
func (a *authenticator) HandleLogout(writer http.ResponseWriter, request *http.Request) {
	a.handleLogout(writer, request)
}
