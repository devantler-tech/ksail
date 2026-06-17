package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	// sessionCookieName holds the signed session issued after a successful OIDC login.
	sessionCookieName = "ksail_session"
	// stateCookieName holds the signed CSRF state and nonce during the login round-trip.
	stateCookieName = "ksail_oidc_state"
	// defaultSessionTTL is how long an authenticated session remains valid.
	defaultSessionTTL = 8 * time.Hour
	// stateCookieTTL bounds how long a login attempt may take.
	stateCookieTTL = 10 * time.Minute
	// randomTokenLength is the byte length of generated state and nonce values.
	randomTokenLength = 16
	// loginPath is where the SPA sends unauthenticated users to start the OIDC flow.
	loginPath = "/api/v1/auth/login"
)

// ErrOIDCConfig is returned when the OIDC configuration is incomplete.
var ErrOIDCConfig = errors.New("invalid OIDC configuration")

var (
	errInvalidCookie = errors.New("invalid signed cookie")
	errStateMismatch = errors.New("oidc state mismatch")
	errMissingCode   = errors.New("missing authorization code")
	errNoIDToken     = errors.New("no id_token in token response")
	errNonceMismatch = errors.New("oidc nonce mismatch")
)

// OIDCConfig configures the operator's app-driven (Headlamp-style) OIDC authentication: the REST
// API itself owns the login/callback as a confidential client. The browser never holds the client
// secret or provider tokens; it rides a signed, HttpOnly session cookie.
type OIDCConfig struct {
	// IssuerURL is the OIDC issuer (discovery) URL. Empty disables authentication.
	IssuerURL string
	// ClientID is the OIDC client identifier registered with the provider.
	ClientID string
	// ClientSecret is the confidential-client secret, used only server-side for the code exchange.
	ClientSecret string
	// RedirectURL is the externally reachable callback (.../api/v1/auth/callback).
	RedirectURL string
	// Scopes are requested in addition to the always-included openid scope.
	Scopes []string
	// SessionSecret signs the session and state cookies (HMAC-SHA256).
	SessionSecret []byte
	// SessionTTL overrides how long a session is valid (zero uses the default).
	SessionTTL time.Duration
	// SecureCookies marks issued cookies Secure (set when reachable over HTTPS).
	SecureCookies bool
}

// Enabled reports whether OIDC authentication is configured.
func (c OIDCConfig) Enabled() bool {
	return c.IssuerURL != ""
}

// sessionClaims is the authenticated identity persisted in the signed session cookie.
type sessionClaims struct {
	Subject string `json:"sub"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Expiry  int64  `json:"exp"`
}

// stateData is the CSRF state and OIDC nonce persisted in the signed state cookie.
type stateData struct {
	State string `json:"state"`
	Nonce string `json:"nonce"`
}

// authenticator runs the server-side OIDC authorization-code flow and issues signed session
// cookies. The operator continues to act with its own RBAC: OIDC provides authentication, not
// per-user authorization.
type authenticator struct {
	config       OIDCConfig
	oauth2Config *oauth2.Config
	verifier     *gooidc.IDTokenVerifier
}

// newAuthenticator discovers the OIDC provider and builds an authenticator. It performs network
// I/O against the issuer, so it is called once at server start.
func newAuthenticator(ctx context.Context, cfg OIDCConfig) (*authenticator, error) {
	if cfg.ClientID == "" || cfg.RedirectURL == "" || len(cfg.SessionSecret) == 0 {
		return nil, fmt.Errorf(
			"%w: clientID, redirectURL and sessionSecret are required",
			ErrOIDCConfig,
		)
	}

	provider, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}

	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = defaultSessionTTL
	}

	scopes := append([]string{gooidc.ScopeOpenID}, cfg.Scopes...)

	return &authenticator{
		config: cfg,
		oauth2Config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
		},
		verifier: provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// handleLogin starts the OIDC flow: it issues a signed state cookie and redirects to the provider.
func (a *authenticator) handleLogin(writer http.ResponseWriter, request *http.Request) {
	state, err := randomToken()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)

		return
	}

	nonce, err := randomToken()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)

		return
	}

	http.SetCookie(writer, a.signedCookie(
		stateCookieName,
		mustMarshal(stateData{State: state, Nonce: nonce}),
		"/api/v1/auth",
		int(stateCookieTTL.Seconds()),
	))

	authURL := a.oauth2Config.AuthCodeURL(state, gooidc.Nonce(nonce))
	http.Redirect(writer, request, authURL, http.StatusFound)
}

// handleCallback completes the OIDC flow: it validates state, exchanges the code, verifies the ID
// token and nonce, and issues the session cookie.
func (a *authenticator) handleCallback(writer http.ResponseWriter, request *http.Request) {
	state, ok := a.readState(request)
	if !ok {
		writeError(writer, http.StatusBadRequest, errInvalidCookie)

		return
	}

	query := request.URL.Query()
	if errMsg := query.Get("error"); errMsg != "" {
		writeError(writer, http.StatusBadRequest, fmt.Errorf("%w: %s", ErrOIDCConfig, errMsg))

		return
	}

	if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state.State)) != 1 {
		writeError(writer, http.StatusBadRequest, errStateMismatch)

		return
	}

	claims, err := a.exchange(request.Context(), query.Get("code"), state.Nonce)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err)

		return
	}

	http.SetCookie(writer, a.signedCookie(
		sessionCookieName,
		mustMarshal(claims),
		"/",
		int(a.config.SessionTTL.Seconds()),
	))
	http.SetCookie(writer, expiredCookie(stateCookieName, "/api/v1/auth", a.config.SecureCookies))
	http.Redirect(writer, request, "/", http.StatusFound)
}

// handleLogout clears the session cookie.
func (a *authenticator) handleLogout(writer http.ResponseWriter, _ *http.Request) {
	http.SetCookie(writer, expiredCookie(sessionCookieName, "/", a.config.SecureCookies))
	writer.WriteHeader(http.StatusNoContent)
}

// exchange swaps the authorization code for tokens and returns the verified session identity.
func (a *authenticator) exchange(
	ctx context.Context,
	code, nonce string,
) (sessionClaims, error) {
	if code == "" {
		return sessionClaims{}, errMissingCode
	}

	token, err := a.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return sessionClaims{}, fmt.Errorf("exchange authorization code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return sessionClaims{}, errNoIDToken
	}

	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return sessionClaims{}, fmt.Errorf("verify id_token: %w", err)
	}

	if idToken.Nonce != nonce {
		return sessionClaims{}, errNonceMismatch
	}

	var profile struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}

	_ = idToken.Claims(&profile)

	return sessionClaims{
		Subject: idToken.Subject,
		Email:   profile.Email,
		Name:    profile.Name,
		Expiry:  time.Now().Add(a.config.SessionTTL).Unix(),
	}, nil
}

// currentUser returns the authenticated identity from a valid, unexpired session cookie.
func (a *authenticator) currentUser(request *http.Request) (sessionClaims, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return sessionClaims{}, false
	}

	payload, err := verifySignedValue(a.config.SessionSecret, cookie.Value)
	if err != nil {
		return sessionClaims{}, false
	}

	var claims sessionClaims

	err = json.Unmarshal(payload, &claims)
	if err != nil {
		return sessionClaims{}, false
	}

	if time.Now().Unix() >= claims.Expiry {
		return sessionClaims{}, false
	}

	return claims, true
}

// readState reads and verifies the signed state cookie.
func (a *authenticator) readState(request *http.Request) (stateData, bool) {
	cookie, err := request.Cookie(stateCookieName)
	if err != nil {
		return stateData{}, false
	}

	payload, err := verifySignedValue(a.config.SessionSecret, cookie.Value)
	if err != nil {
		return stateData{}, false
	}

	var state stateData

	err = json.Unmarshal(payload, &state)
	if err != nil {
		return stateData{}, false
	}

	return state, true
}

// newAuthCookie builds an auth cookie shared by signedCookie and expiredCookie.
// All auth cookies are HttpOnly with SameSite=Lax. Secure is caller-driven:
// true behind HTTPS, false for local HTTP port-forwards (a Secure cookie would
// otherwise be dropped by the browser over plain HTTP).
func newAuthCookie(name, value, path string, maxAge int, secure bool) *http.Cookie {
	// G124 false positive: HttpOnly + SameSite=Lax are set; Secure is caller-driven
	// (false only for local HTTP port-forwards, where a Secure cookie is dropped).

	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// signedCookie builds an HttpOnly cookie whose value is the HMAC-signed payload.
func (a *authenticator) signedCookie(
	name string,
	payload []byte,
	path string,
	maxAge int,
) *http.Cookie {
	return newAuthCookie(
		name,
		signValue(a.config.SessionSecret, payload),
		path,
		maxAge,
		a.config.SecureCookies,
	)
}

func expiredCookie(name, path string, secure bool) *http.Cookie {
	return newAuthCookie(name, "", path, -1, secure)
}

// signValue returns "<base64(payload)>.<base64(hmac)>" using HMAC-SHA256.
func signValue(secret, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)

	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifySignedValue checks the HMAC signature and returns the original payload.
func verifySignedValue(secret []byte, value string) ([]byte, error) {
	encodedPayload, encodedSig, found := strings.Cut(value, ".")
	if !found {
		return nil, errInvalidCookie
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, errInvalidCookie
	}

	signature, err := base64.RawURLEncoding.DecodeString(encodedSig)
	if err != nil {
		return nil, errInvalidCookie
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)

	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, errInvalidCookie
	}

	return payload, nil
}

func randomToken() (string, error) {
	buffer := make([]byte, randomTokenLength)

	_, err := rand.Read(buffer)
	if err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func mustMarshal(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		// The inputs are fixed local structs with only string/int fields, so marshaling cannot fail.
		panic(fmt.Sprintf("marshal cookie payload: %v", err))
	}

	return data
}
