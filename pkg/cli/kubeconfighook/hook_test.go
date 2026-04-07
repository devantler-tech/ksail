package kubeconfighook_test

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfighook"
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

func TestIsTokenExpired_TimeBased(t *testing.T) {
	t.Parallel()

	t.Run("ExpiredToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiredAt := time.Now().Add(-1 * time.Hour).Unix()
		token := makeJWT(t, expiredAt)
		path := writeKubeconfig(t, dir, token)

		assert.True(t, kubeconfighook.IsTokenExpired(path))
	})

	t.Run("ValidToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiresAt := time.Now().Add(24 * time.Hour).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, token)

		assert.False(t, kubeconfighook.IsTokenExpired(path))
	})

	t.Run("TokenExpiringWithinBuffer", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiresAt := time.Now().Add(2 * time.Minute).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, token)

		assert.True(t, kubeconfighook.IsTokenExpired(path))
	})

	t.Run("TokenExpiringOutsideBuffer", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiresAt := time.Now().Add(10 * time.Minute).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, token)

		assert.False(t, kubeconfighook.IsTokenExpired(path))
	})
}

func TestIsTokenExpired_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("NonJWTToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "plain-bearer-token")

		assert.False(t, kubeconfighook.IsTokenExpired(path))
	})

	t.Run("TwoSegmentToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "header.payload")

		assert.False(t, kubeconfighook.IsTokenExpired(path))
	})

	t.Run("FourSegmentToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "a.b.c.d")

		assert.False(t, kubeconfighook.IsTokenExpired(path))
	})

	t.Run("NoToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "")

		assert.False(t, kubeconfighook.IsTokenExpired(path))
	})

	t.Run("MissingFile", func(t *testing.T) {
		t.Parallel()

		assert.False(t, kubeconfighook.IsTokenExpired("/nonexistent/kubeconfig"))
	})

	t.Run("MissingContext", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		kubeconfigPath := filepath.Join(dir, "kubeconfig")

		cfg := clientcmdapi.NewConfig()
		cfg.CurrentContext = "missing"

		err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
		require.NoError(t, err)

		assert.False(t, kubeconfighook.IsTokenExpired(kubeconfigPath))
	})
}
