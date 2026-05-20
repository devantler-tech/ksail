package oidc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCallbackRequest(t *testing.T, query url.Values) *http.Request {
	t.Helper()

	return httptest.NewRequest(http.MethodGet, "/callback?"+query.Encode(), nil)
}

func TestValidateCallbackRequest(t *testing.T) {
	t.Parallel()

	const expectedState = "expected-state"

	t.Run("returns code on success", func(t *testing.T) {
		t.Parallel()

		req := newCallbackRequest(t, url.Values{
			"state": {expectedState},
			"code":  {"auth-code-123"},
		})

		code, err := oidc.ValidateCallbackRequestForTest(req, expectedState)

		require.NoError(t, err)
		assert.Equal(t, "auth-code-123", code)
	})

	t.Run("returns error when provider reports error", func(t *testing.T) {
		t.Parallel()

		req := newCallbackRequest(t, url.Values{
			"error":             {"access_denied"},
			"error_description": {"user declined"},
		})

		_, err := oidc.ValidateCallbackRequestForTest(req, expectedState)

		require.ErrorIs(t, err, oidc.ErrAuthenticationFailed)
		assert.Contains(t, err.Error(), "access_denied")
		assert.Contains(t, err.Error(), "user declined")
	})

	t.Run("returns error on state mismatch", func(t *testing.T) {
		t.Parallel()

		req := newCallbackRequest(t, url.Values{
			"state": {"wrong-state"},
			"code":  {"auth-code-123"},
		})

		_, err := oidc.ValidateCallbackRequestForTest(req, expectedState)

		require.ErrorIs(t, err, oidc.ErrAuthenticationFailed)
		assert.Contains(t, err.Error(), "state mismatch")
	})

	t.Run("returns error when code is missing", func(t *testing.T) {
		t.Parallel()

		req := newCallbackRequest(t, url.Values{
			"state": {expectedState},
		})

		_, err := oidc.ValidateCallbackRequestForTest(req, expectedState)

		require.ErrorIs(t, err, oidc.ErrAuthenticationFailed)
		assert.Contains(t, err.Error(), "missing authorization code")
	})
}

func TestGeneratePKCE(t *testing.T) {
	t.Parallel()

	verifier, challenge, err := oidc.GeneratePKCEForTest()
	require.NoError(t, err)

	assert.NotEmpty(t, verifier)
	assert.NotEmpty(t, challenge)
	assert.NotEqual(t, verifier, challenge)

	// The challenge must be the base64url-encoded SHA-256 of the verifier.
	digest := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(digest[:])
	assert.Equal(t, expectedChallenge, challenge)

	// Successive calls must produce different verifiers.
	verifier2, _, err := oidc.GeneratePKCEForTest()
	require.NoError(t, err)
	assert.NotEqual(t, verifier, verifier2)
}

func TestGenerateState(t *testing.T) {
	t.Parallel()

	state, err := oidc.GenerateStateForTest()
	require.NoError(t, err)
	assert.NotEmpty(t, state)

	// State must decode to 16 random bytes.
	decoded, err := base64.RawURLEncoding.DecodeString(state)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)

	state2, err := oidc.GenerateStateForTest()
	require.NoError(t, err)
	assert.NotEqual(t, state, state2)
}

// writeTestCAPEM writes a freshly generated self-signed CA certificate to a temp
// file and returns its path.
func writeTestCAPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ksail-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))

	return path
}

func TestBuildHTTPClient(t *testing.T) {
	t.Parallel()

	t.Run("returns default client when no CA file", func(t *testing.T) {
		t.Parallel()

		auth := &oidc.Authenticator{}

		client, err := auth.BuildHTTPClientForTest()

		require.NoError(t, err)
		assert.Same(t, http.DefaultClient, client)
	})

	t.Run("errors when CA file does not exist", func(t *testing.T) {
		t.Parallel()

		auth := &oidc.Authenticator{CAFile: filepath.Join(t.TempDir(), "missing.pem")}

		_, err := auth.BuildHTTPClientForTest()

		require.Error(t, err)
	})

	t.Run("errors when CA file is not a valid certificate", func(t *testing.T) {
		t.Parallel()

		badPath := filepath.Join(t.TempDir(), "bad.pem")
		require.NoError(t, os.WriteFile(badPath, []byte("not a certificate"), 0o600))

		auth := &oidc.Authenticator{CAFile: badPath}

		_, err := auth.BuildHTTPClientForTest()

		require.ErrorIs(t, err, oidc.ErrAuthenticationFailed)
		assert.Contains(t, err.Error(), "failed to parse CA certificate")
	})

	t.Run("builds custom client from valid CA file", func(t *testing.T) {
		t.Parallel()

		auth := &oidc.Authenticator{CAFile: writeTestCAPEM(t)}

		client, err := auth.BuildHTTPClientForTest()

		require.NoError(t, err)
		require.NotNil(t, client)
		assert.NotSame(t, http.DefaultClient, client)

		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok, "expected *http.Transport")
		require.NotNil(t, transport.TLSClientConfig)
		assert.NotNil(t, transport.TLSClientConfig.RootCAs)
	})
}

func TestNewOIDCProvider(t *testing.T) {
	t.Parallel()

	t.Run("succeeds against a valid discovery document", func(t *testing.T) {
		t.Parallel()

		var issuer string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/openid-configuration" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 issuer,
					"authorization_endpoint": issuer + "/auth",
					"token_endpoint":         issuer + "/token",
					"jwks_uri":               issuer + "/keys",
				})

				return
			}

			http.NotFound(w, r)
		}))
		defer server.Close()

		issuer = server.URL

		auth := &oidc.Authenticator{IssuerURL: issuer, ClientID: "kubectl"}

		err := auth.NewOIDCProviderForTest(context.Background(), "http://localhost:18000/callback")

		require.NoError(t, err)
	})

	t.Run("errors when discovery fails", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer server.Close()

		auth := &oidc.Authenticator{IssuerURL: server.URL, ClientID: "kubectl"}

		err := auth.NewOIDCProviderForTest(context.Background(), "http://localhost:18000/callback")

		require.ErrorIs(t, err, oidc.ErrAuthenticationFailed)
		assert.Contains(t, err.Error(), "failed to discover OIDC provider")
	})
}
