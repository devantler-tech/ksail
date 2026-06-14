package api_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	flowSecret       = "0123456789abcdef0123456789abcdef"
	testSubject      = "alice"
	flowClientSecret = "client-secret"
	flowRedirectURL  = "https://app.example/api/v1/auth/callback"
)

// mockIDP is a minimal OIDC provider (discovery + JWKS + token endpoints) for exercising the
// server-side authorization-code flow without a real identity provider.
type mockIDP struct {
	server   *httptest.Server
	signer   jose.Signer
	keyID    string
	clientID string
	subject  string
	email    string
	name     string
	nonce    string // embedded in the next issued id_token

	// Optional knobs for exercising callback failure modes (zero values preserve the happy path).
	tokenStatus int           // HTTP status for the token endpoint (0 == 200 OK)
	omitIDToken bool          // when true the token response carries no id_token
	idTokenTTL  time.Duration // id_token lifetime relative to now (0 == 1h)
}

func newMockIDP(t *testing.T) *mockIDP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	idp := &mockIDP{
		keyID:    "test-key",
		clientID: "ksail",
		subject:  testSubject,
		email:    "alice@example.com",
		name:     "Alice",
	}

	signingKey := jose.JSONWebKey{
		Key:       key,
		KeyID:     idp.keyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: signingKey},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	require.NoError(t, err)

	idp.signer = signer
	idp.server = httptest.NewServer(idp.routes(key.Public()))

	t.Cleanup(idp.server.Close)

	return idp
}

func (idp *mockIDP) routes(pub crypto.PublicKey) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(
		"/.well-known/openid-configuration",
		func(writer http.ResponseWriter, _ *http.Request) {
			writeJSONRaw(writer, map[string]any{
				"issuer":                                idp.server.URL,
				"authorization_endpoint":                idp.server.URL + "/auth",
				"token_endpoint":                        idp.server.URL + "/token",
				"jwks_uri":                              idp.server.URL + "/keys",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		},
	)

	mux.HandleFunc("/keys", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONRaw(writer, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: pub, KeyID: idp.keyID, Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})

	mux.HandleFunc("/token", func(writer http.ResponseWriter, _ *http.Request) {
		if idp.tokenStatus != 0 {
			writer.WriteHeader(idp.tokenStatus)

			return
		}

		response := map[string]any{
			"access_token": "access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		if !idp.omitIDToken {
			response["id_token"] = idp.signIDToken()
		}

		writeJSONRaw(writer, response)
	})

	return mux
}

func (idp *mockIDP) signIDToken() string {
	ttl := idp.idTokenTTL
	if ttl == 0 {
		ttl = time.Hour
	}

	now := time.Now()
	claims := map[string]any{
		"iss":   idp.server.URL,
		"sub":   idp.subject,
		"aud":   idp.clientID,
		"exp":   now.Add(ttl).Unix(),
		"iat":   now.Unix(),
		"nonce": idp.nonce,
		"email": idp.email,
		"name":  idp.name,
	}

	raw, err := jwt.Signed(idp.signer).Claims(claims).Serialize()
	if err != nil {
		panic(err)
	}

	return raw
}

func writeJSONRaw(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(writer).Encode(value)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}

func TestEnabled(t *testing.T) {
	t.Parallel()

	assert.False(t, api.OIDCConfig{}.Enabled())
	assert.True(t, api.OIDCConfig{IssuerURL: "https://issuer.example"}.Enabled())
}

func TestNewAuthenticatorRequiresConfig(t *testing.T) {
	t.Parallel()

	_, err := api.NewAuthenticator(
		context.Background(),
		api.OIDCConfig{IssuerURL: "https://issuer.example"},
	)
	require.ErrorIs(t, err, api.ErrOIDCConfig)
}

func TestOIDCLoginCallbackFlow(t *testing.T) {
	t.Parallel()

	idp := newMockIDP(t)
	secret := []byte(flowSecret)

	auth, err := api.NewAuthenticator(context.Background(), api.OIDCConfig{
		IssuerURL:     idp.server.URL,
		ClientID:      idp.clientID,
		ClientSecret:  flowClientSecret,
		RedirectURL:   flowRedirectURL,
		Scopes:        []string{"email", "profile"},
		SessionSecret: secret,
		SessionTTL:    time.Hour,
	})
	require.NoError(t, err)

	// 1. Login redirects to the provider and sets a signed state cookie.
	loginRec := httptest.NewRecorder()
	auth.HandleLogin(loginRec, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, api.LoginPath, nil,
	))
	require.Equal(t, http.StatusFound, loginRec.Code)

	stateCookie := findCookie(loginRec.Result().Cookies(), api.StateCookieName)
	require.NotNil(t, stateCookie)

	statePayload, err := api.VerifySignedValue(secret, stateCookie.Value)
	require.NoError(t, err)

	var state api.StateData

	require.NoError(t, json.Unmarshal(statePayload, &state))
	assert.Contains(t, loginRec.Header().Get("Location"), "state="+state.State)

	// The token endpoint must echo the nonce chosen during login.
	idp.nonce = state.Nonce

	// 2. Callback exchanges the code and issues a session cookie.
	callbackReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/auth/callback?code=auth-code&state="+url.QueryEscape(state.State),
		nil,
	)
	callbackReq.AddCookie(stateCookie)

	callbackRec := httptest.NewRecorder()
	auth.HandleCallback(callbackRec, callbackReq)
	require.Equal(t, http.StatusFound, callbackRec.Code, callbackRec.Body.String())

	sessionCookie := findCookie(callbackRec.Result().Cookies(), api.SessionCookieName)
	require.NotNil(t, sessionCookie)

	// 3. The session cookie authenticates subsequent requests.
	authedReq := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/clusters", nil,
	)
	authedReq.AddCookie(sessionCookie)

	claims, ok := auth.CurrentUser(authedReq)
	require.True(t, ok)
	assert.Equal(t, testSubject, claims.Subject)
	assert.Equal(t, "alice@example.com", claims.Email)
}

func TestHandleCallbackRejectsBadRequests(t *testing.T) {
	t.Parallel()

	secret := []byte(flowSecret)
	auth := api.NewConfigAuthenticator(api.OIDCConfig{SessionSecret: secret, SessionTTL: time.Hour})

	goodState := auth.SignedCookie(
		api.StateCookieName,
		api.MustMarshal(api.StateData{State: "good", Nonce: "n"}),
		"/api/v1/auth",
		600,
	)

	tests := map[string]struct {
		target     string
		withCookie bool
	}{
		"missing state cookie": {
			target:     "/api/v1/auth/callback?code=c&state=good",
			withCookie: false,
		},
		"state mismatch": {
			target:     "/api/v1/auth/callback?code=c&state=bad",
			withCookie: true,
		},
		"provider error": {
			target:     "/api/v1/auth/callback?error=access_denied&state=good",
			withCookie: true,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, testCase.target, nil,
			)
			if testCase.withCookie {
				req.AddCookie(goodState)
			}

			rec := httptest.NewRecorder()
			auth.HandleCallback(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestHandleLogoutClearsSession(t *testing.T) {
	t.Parallel()

	auth := api.NewConfigAuthenticator(api.OIDCConfig{SessionSecret: []byte(flowSecret)})

	rec := httptest.NewRecorder()
	auth.HandleLogout(rec, httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/logout", nil,
	))

	require.Equal(t, http.StatusNoContent, rec.Code)

	cleared := findCookie(rec.Result().Cookies(), api.SessionCookieName)
	require.NotNil(t, cleared)
	assert.Negative(t, cleared.MaxAge)
}
