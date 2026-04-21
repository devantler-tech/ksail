package kubeconfighook_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfighook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// makeJWT creates a minimal JWT token with the given expiry timestamp.
func makeJWT(t *testing.T, exp int64) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	claims := map[string]any{"exp": exp}
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)

	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))

	return header + "." + payload + "." + sig
}

// writeKubeconfig writes a kubeconfig file with a fixed context and the given token.
func writeKubeconfig(t *testing.T, dir, token string) string {
	t.Helper()

	const contextName = "my-cluster"

	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	cfg := clientcmdapi.NewConfig()
	cfg.CurrentContext = contextName
	cfg.Clusters[contextName] = &clientcmdapi.Cluster{
		Server: "https://127.0.0.1:6443",
	}
	cfg.AuthInfos[contextName] = &clientcmdapi.AuthInfo{
		Token: token,
	}
	cfg.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  contextName,
		AuthInfo: contextName,
	}

	err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
	require.NoError(t, err)

	return kubeconfigPath
}

// writeKubeconfigWithServer writes a kubeconfig pointing at the given server URL.
func writeKubeconfigWithServer(t *testing.T, dir, serverURL, token string) string {
	t.Helper()

	const contextName = "test"

	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	cfg := clientcmdapi.NewConfig()
	cfg.CurrentContext = contextName
	cfg.Clusters[contextName] = &clientcmdapi.Cluster{
		Server:                serverURL,
		InsecureSkipTLSVerify: true,
	}
	cfg.AuthInfos[contextName] = &clientcmdapi.AuthInfo{
		Token: token,
	}
	cfg.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  contextName,
		AuthInfo: contextName,
	}

	err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
	require.NoError(t, err)

	return kubeconfigPath
}

func TestIsTokenExpired_TimeBased(t *testing.T) {
	t.Parallel()

	t.Run("ExpiredToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiredAt := time.Now().Add(-1 * time.Hour).Unix()
		token := makeJWT(t, expiredAt)
		path := writeKubeconfig(t, dir, token)

		assert.True(t, kubeconfighook.IsTokenExpired(path, ""))
	})

	t.Run("ValidToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiresAt := time.Now().Add(24 * time.Hour).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, token)

		assert.False(t, kubeconfighook.IsTokenExpired(path, ""))
	})

	t.Run("TokenExpiringWithinBuffer", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiresAt := time.Now().Add(2 * time.Minute).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, token)

		assert.True(t, kubeconfighook.IsTokenExpired(path, ""))
	})

	t.Run("TokenExpiringOutsideBuffer", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiresAt := time.Now().Add(10 * time.Minute).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, token)

		assert.False(t, kubeconfighook.IsTokenExpired(path, ""))
	})
}

func TestIsTokenExpired_ExplicitContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	validToken := makeJWT(t, time.Now().Add(24*time.Hour).Unix())
	expiredToken := makeJWT(t, time.Now().Add(-1*time.Hour).Unix())

	cfg := clientcmdapi.NewConfig()
	cfg.CurrentContext = "valid-ctx"
	cfg.Clusters["valid-ctx"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:6443"}
	cfg.AuthInfos["valid-user"] = &clientcmdapi.AuthInfo{Token: validToken}
	cfg.Contexts["valid-ctx"] = &clientcmdapi.Context{
		Cluster: "valid-ctx", AuthInfo: "valid-user",
	}
	cfg.Clusters["expired-ctx"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:6444"}
	cfg.AuthInfos["expired-user"] = &clientcmdapi.AuthInfo{Token: expiredToken}
	cfg.Contexts["expired-ctx"] = &clientcmdapi.Context{
		Cluster: "expired-ctx", AuthInfo: "expired-user",
	}

	err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
	require.NoError(t, err)

	// CurrentContext has a valid token
	assert.False(t, kubeconfighook.IsTokenExpired(kubeconfigPath, ""))
	// Explicit context has an expired token
	assert.True(t, kubeconfighook.IsTokenExpired(kubeconfigPath, "expired-ctx"))
}

func TestIsTokenExpired_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("NonJWTToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "plain-bearer-token")

		assert.False(t, kubeconfighook.IsTokenExpired(path, ""))
	})

	t.Run("TwoSegmentToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "header.payload")

		assert.False(t, kubeconfighook.IsTokenExpired(path, ""))
	})

	t.Run("FourSegmentToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "a.b.c.d")

		assert.False(t, kubeconfighook.IsTokenExpired(path, ""))
	})

	t.Run("NoToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "")

		assert.False(t, kubeconfighook.IsTokenExpired(path, ""))
	})

	t.Run("MissingFile", func(t *testing.T) {
		t.Parallel()

		assert.False(t, kubeconfighook.IsTokenExpired("/nonexistent/kubeconfig", ""))
	})

	t.Run("MissingContext", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		kubeconfigPath := filepath.Join(dir, "kubeconfig")

		cfg := clientcmdapi.NewConfig()
		cfg.CurrentContext = "missing"

		err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
		require.NoError(t, err)

		assert.False(t, kubeconfighook.IsTokenExpired(kubeconfigPath, ""))
	})
}

func TestIsKubeconfigStale_AuthErrors(t *testing.T) {
	t.Parallel()

	t.Run("Unauthorized401", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w,
				`{"kind":"Status","apiVersion":"v1","status":"Failure",`+
					`"message":"Unauthorized","reason":"Unauthorized","code":401}`)
		}))
		defer srv.Close()

		dir := t.TempDir()
		path := writeKubeconfigWithServer(t, dir, srv.URL, "stale-token")

		assert.True(t, kubeconfighook.IsKubeconfigStale(path, ""))
	})

	t.Run("Forbidden403", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w,
				`{"kind":"Status","apiVersion":"v1","status":"Failure",`+
					`"message":"Forbidden","reason":"Forbidden","code":403}`)
		}))
		defer srv.Close()

		dir := t.TempDir()
		path := writeKubeconfigWithServer(t, dir, srv.URL, "stale-token")

		assert.True(t, kubeconfighook.IsKubeconfigStale(path, ""))
	})
}

func TestIsKubeconfigStale_NonStale(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulResponse", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"major":"1","minor":"30","gitVersion":"v1.30.0"}`)
		}))
		defer srv.Close()

		dir := t.TempDir()
		path := writeKubeconfigWithServer(t, dir, srv.URL, "valid-token")

		assert.False(t, kubeconfighook.IsKubeconfigStale(path, ""))
	})

	t.Run("ConnectionRefused", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// Port 1 is unlikely to be listening
		path := writeKubeconfigWithServer(t, dir, "https://127.0.0.1:1", "some-token")

		assert.False(t, kubeconfighook.IsKubeconfigStale(path, ""))
	})
}

func TestIsKubeconfigStale_ConfigEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("MissingFile", func(t *testing.T) {
		t.Parallel()

		assert.True(t, kubeconfighook.IsKubeconfigStale("/nonexistent/kubeconfig", ""))
	})

	t.Run("MissingContext", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfigWithServer(t, dir, "https://127.0.0.1:6443", "token")

		// Request a context that doesn't exist in the kubeconfig
		assert.True(t, kubeconfighook.IsKubeconfigStale(path, "nonexistent-context"))
	})
}

func TestIsKubeconfigStale_ExplicitContext(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"major":"1","minor":"30","gitVersion":"v1.30.0"}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	cfg := clientcmdapi.NewConfig()
	cfg.CurrentContext = "default"
	cfg.Clusters["default"] = &clientcmdapi.Cluster{
		Server:                "https://127.0.0.1:1", // unreachable
		InsecureSkipTLSVerify: true,
	}
	cfg.AuthInfos["default"] = &clientcmdapi.AuthInfo{Token: "tok"}
	cfg.Contexts["default"] = &clientcmdapi.Context{
		Cluster: "default", AuthInfo: "default",
	}
	cfg.Clusters["reachable"] = &clientcmdapi.Cluster{
		Server:                srv.URL,
		InsecureSkipTLSVerify: true,
	}
	cfg.AuthInfos["reachable"] = &clientcmdapi.AuthInfo{Token: "tok"}
	cfg.Contexts["reachable"] = &clientcmdapi.Context{
		Cluster: "reachable", AuthInfo: "reachable",
	}

	err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
	require.NoError(t, err)

	// Explicit context points to the reachable server → not stale
	assert.False(t, kubeconfighook.IsKubeconfigStale(kubeconfigPath, "reachable"))
}
