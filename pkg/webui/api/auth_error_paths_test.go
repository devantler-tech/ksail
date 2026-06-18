package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFlowAuthenticator builds a live authenticator discovered against the mock IDP.
func newFlowAuthenticator(t *testing.T, idp *mockIDP) *api.Authenticator {
	t.Helper()

	auth, err := api.NewAuthenticator(context.Background(), api.OIDCConfig{
		IssuerURL:     idp.server.URL,
		ClientID:      idp.clientID,
		ClientSecret:  flowClientSecret,
		RedirectURL:   flowRedirectURL,
		SessionSecret: []byte(flowSecret),
		SessionTTL:    time.Hour,
	})
	require.NoError(t, err)

	return auth
}

// startLogin runs handleLogin and returns the issued state cookie plus its decoded contents.
func startLogin(t *testing.T, auth *api.Authenticator) (*http.Cookie, api.StateData) {
	t.Helper()

	rec := httptest.NewRecorder()
	auth.HandleLogin(rec, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, api.LoginPath, nil,
	))
	require.Equal(t, http.StatusFound, rec.Code)

	cookie := findCookie(rec.Result().Cookies(), api.StateCookieName)
	require.NotNil(t, cookie)

	payload, err := api.VerifySignedValue([]byte(flowSecret), cookie.Value)
	require.NoError(t, err)

	var state api.StateData

	require.NoError(t, json.Unmarshal(payload, &state))

	return cookie, state
}

// corruptedCookie returns a cookie whose value is HMAC-signed with the wrong secret, so verifying
// it against flowSecret fails — a stand-in for a tampered or forged cookie.
func corruptedCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    api.SignValue([]byte("ffffffffffffffffffffffffffffffff"), []byte("{}")),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// callback drives handleCallback with an optional state cookie and returns the recorder.
func callback(
	t *testing.T,
	auth *api.Authenticator,
	stateCookie *http.Cookie,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if stateCookie != nil {
		req.AddCookie(stateCookie)
	}

	rec := httptest.NewRecorder()
	auth.HandleCallback(rec, req)

	return rec
}

// TestNewAuthenticatorDiscoveryFailure covers the provider-discovery error path: the config is
// complete but the issuer is unreachable, so NewProvider (network I/O) fails.
func TestNewAuthenticatorDiscoveryFailure(t *testing.T) {
	t.Parallel()

	_, err := api.NewAuthenticator(context.Background(), api.OIDCConfig{
		IssuerURL:     "https://127.0.0.1:1/unreachable-issuer",
		ClientID:      "ksail",
		RedirectURL:   flowRedirectURL,
		SessionSecret: []byte(flowSecret),
	})
	require.Error(t, err)
	// A discovery (network) failure, not a config-validation error.
	require.NotErrorIs(t, err, api.ErrOIDCConfig)
}

// TestNewAuthenticatorDefaultsSessionTTL covers the SessionTTL defaulting branch: when the config
// omits SessionTTL the issued session cookie must live for the 8h default.
func TestNewAuthenticatorDefaultsSessionTTL(t *testing.T) {
	t.Parallel()

	idp := newMockIDP(t)

	auth, err := api.NewAuthenticator(context.Background(), api.OIDCConfig{
		IssuerURL:     idp.server.URL,
		ClientID:      idp.clientID,
		ClientSecret:  flowClientSecret,
		RedirectURL:   flowRedirectURL,
		SessionSecret: []byte(flowSecret),
		// SessionTTL intentionally unset → defaults to 8h.
	})
	require.NoError(t, err)

	stateCookie, state := startLogin(t, auth)
	idp.nonce = state.Nonce

	rec := callback(
		t, auth, stateCookie,
		"/api/v1/auth/callback?code=auth-code&state="+url.QueryEscape(state.State),
	)
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())

	session := findCookie(rec.Result().Cookies(), api.SessionCookieName)
	require.NotNil(t, session)
	assert.Equal(t, int((8 * time.Hour).Seconds()), session.MaxAge)
}

// TestHandleCallbackEmptyCodeUnauthorized covers the missing-code path: state validates but no
// authorization code is present, so exchange short-circuits and the callback returns 401.
func TestHandleCallbackEmptyCodeUnauthorized(t *testing.T) {
	t.Parallel()

	auth := api.NewConfigAuthenticator(api.OIDCConfig{
		SessionSecret: []byte(flowSecret),
		SessionTTL:    time.Hour,
	})
	stateCookie := auth.SignedCookie(
		api.StateCookieName,
		api.MustMarshal(api.StateData{State: "s", Nonce: "n"}),
		"/api/v1/auth",
		600,
	)

	rec := callback(t, auth, stateCookie, "/api/v1/auth/callback?state=s") // no code parameter
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// TestHandleCallbackRejectsProviderFailures covers the code-exchange failure paths: a transport
// error, a token response without an id_token, an expired (unverifiable) id_token, and a nonce
// that does not match the login round-trip. Each must surface as 401 Unauthorized.
func TestHandleCallbackRejectsProviderFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]func(idp *mockIDP, state api.StateData){
		"token endpoint error": func(idp *mockIDP, _ api.StateData) {
			idp.tokenStatus = http.StatusBadRequest
		},
		"missing id_token": func(idp *mockIDP, state api.StateData) {
			idp.nonce = state.Nonce
			idp.omitIDToken = true
		},
		"expired id_token": func(idp *mockIDP, state api.StateData) {
			idp.nonce = state.Nonce
			idp.idTokenTTL = -time.Hour
		},
		"nonce mismatch": func(idp *mockIDP, state api.StateData) {
			idp.nonce = state.Nonce + "-tampered"
		},
	}

	for name, configure := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			idp := newMockIDP(t)
			auth := newFlowAuthenticator(t, idp)
			stateCookie, state := startLogin(t, auth)
			configure(idp, state)

			rec := callback(
				t, auth, stateCookie,
				"/api/v1/auth/callback?code=auth-code&state="+url.QueryEscape(state.State),
			)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}

// TestHandleCallbackRejectsCorruptStateCookie covers readState's verification failures: a state
// cookie with a broken signature and one that is correctly signed over a non-JSON payload both
// yield 400 Bad Request. (The missing-cookie case is covered by TestHandleCallbackRejectsBadRequests.)
func TestHandleCallbackRejectsCorruptStateCookie(t *testing.T) {
	t.Parallel()

	auth := api.NewConfigAuthenticator(api.OIDCConfig{
		SessionSecret: []byte(flowSecret),
		SessionTTL:    time.Hour,
	})

	tests := map[string]*http.Cookie{
		"tampered signature": corruptedCookie(api.StateCookieName),
		"valid hmac invalid json": auth.SignedCookie(
			api.StateCookieName, []byte("not-json"), "/api/v1/auth", 600,
		),
	}

	for name, cookie := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := callback(t, auth, cookie, "/api/v1/auth/callback?code=c&state=anything")
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

// TestCurrentUserRejectsInvalidSessionCookies covers currentUser's rejection paths: no cookie, a
// cookie with a broken signature, and a correctly signed cookie over a non-JSON payload.
func TestCurrentUserRejectsInvalidSessionCookies(t *testing.T) {
	t.Parallel()

	auth := api.NewConfigAuthenticator(api.OIDCConfig{
		SessionSecret: []byte(flowSecret),
		SessionTTL:    time.Hour,
	})

	tests := map[string]*http.Cookie{
		"missing cookie":     nil,
		"tampered signature": corruptedCookie(api.SessionCookieName),
		"valid hmac invalid json": auth.SignedCookie(
			api.SessionCookieName, []byte("not-json"), "/", 3600,
		),
	}

	for name, cookie := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, "/api/v1/clusters", nil,
			)
			if cookie != nil {
				req.AddCookie(cookie)
			}

			_, ok := auth.CurrentUser(req)
			assert.False(t, ok)
		})
	}
}
