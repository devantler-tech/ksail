package kubeconfighook

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
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

// writeKubeconfig writes a kubeconfig file with the given context, user, and token.
func writeKubeconfig(t *testing.T, dir, contextName, token string) string {
	t.Helper()

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

func TestIsTokenExpired(t *testing.T) {
	t.Parallel()

	t.Run("ExpiredToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiredAt := time.Now().Add(-1 * time.Hour).Unix()
		token := makeJWT(t, expiredAt)
		path := writeKubeconfig(t, dir, "my-cluster", token)

		assert.True(t, IsTokenExpired(path))
	})

	t.Run("ValidToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		expiresAt := time.Now().Add(24 * time.Hour).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, "my-cluster", token)

		assert.False(t, IsTokenExpired(path))
	})

	t.Run("TokenExpiringWithinBuffer", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// Expires in 2 minutes — within the 5-minute buffer
		expiresAt := time.Now().Add(2 * time.Minute).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, "my-cluster", token)

		assert.True(t, IsTokenExpired(path))
	})

	t.Run("TokenExpiringOutsideBuffer", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// Expires in 10 minutes — outside the 5-minute buffer
		expiresAt := time.Now().Add(10 * time.Minute).Unix()
		token := makeJWT(t, expiresAt)
		path := writeKubeconfig(t, dir, "my-cluster", token)

		assert.False(t, IsTokenExpired(path))
	})

	t.Run("NonJWTToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "my-cluster", "plain-bearer-token")

		assert.False(t, IsTokenExpired(path))
	})

	t.Run("NoToken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "my-cluster", "")

		assert.False(t, IsTokenExpired(path))
	})

	t.Run("MissingFile", func(t *testing.T) {
		t.Parallel()

		assert.False(t, IsTokenExpired("/nonexistent/kubeconfig"))
	})

	t.Run("MissingContext", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		kubeconfigPath := filepath.Join(dir, "kubeconfig")

		cfg := clientcmdapi.NewConfig()
		cfg.CurrentContext = "missing"

		err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
		require.NoError(t, err)

		assert.False(t, IsTokenExpired(kubeconfigPath))
	})
}

func TestJwtExpiry(t *testing.T) {
	t.Parallel()

	t.Run("ValidJWT", func(t *testing.T) {
		t.Parallel()

		expected := time.Now().Add(1 * time.Hour).Unix()
		token := makeJWT(t, expected)

		got, err := jwtExpiry(token)
		require.NoError(t, err)
		assert.Equal(t, expected, got.Unix())
	})

	t.Run("NotAJWT", func(t *testing.T) {
		t.Parallel()

		_, err := jwtExpiry("not-a-jwt")
		assert.ErrorIs(t, err, errNotJWT)
	})

	t.Run("InvalidBase64", func(t *testing.T) {
		t.Parallel()

		_, err := jwtExpiry("header.!!!invalid!!!.sig")
		assert.Error(t, err)
	})

	t.Run("NoExpClaim", func(t *testing.T) {
		t.Parallel()

		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test"}`))

		_, err := jwtExpiry(header + "." + payload + ".sig")
		assert.ErrorIs(t, err, errNoExpClaim)
	})
}

func TestClusterNameFromKubeconfig(t *testing.T) {
	t.Parallel()

	t.Run("ValidKubeconfig", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "my-omni-cluster", "some-token")

		name := clusterNameFromKubeconfig(path)
		assert.Equal(t, "my-omni-cluster", name)
	})

	t.Run("MissingFile", func(t *testing.T) {
		t.Parallel()

		name := clusterNameFromKubeconfig("/nonexistent/kubeconfig")
		assert.Empty(t, name)
	})
}

func TestClusterNameFromDistConfig(t *testing.T) {
	t.Parallel()

	t.Run("NilConfig", func(t *testing.T) {
		t.Parallel()

		name := clusterNameFromDistConfig(nil)
		assert.Empty(t, name)
	})

	t.Run("NoTalosConfig", func(t *testing.T) {
		t.Parallel()

		distCfg := &clusterprovisioner.DistributionConfig{}
		name := clusterNameFromDistConfig(distCfg)
		assert.Empty(t, name)
	})
}

func TestResolveClusterName(t *testing.T) {
	t.Parallel()

	t.Run("FallsBackToKubeconfig", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfig(t, dir, "omni-cluster", "some-token")

		name := resolveClusterName(nil, nil, path)
		assert.Equal(t, "omni-cluster", name)
	})

	t.Run("EmptyWhenNoSources", func(t *testing.T) {
		t.Parallel()

		name := resolveClusterName(nil, nil, "/nonexistent/kubeconfig")
		assert.Empty(t, name)
	})
}

func TestLoadConfigSilently_NoConfig(t *testing.T) {
	t.Parallel()

	// Run in a temp dir with no ksail.yaml
	origDir, err := os.Getwd()
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))

	t.Cleanup(func() {
		require.NoError(t, os.Chdir(origDir))
	})

	cfg, distCfg := loadConfigSilently(nil)
	assert.Nil(t, cfg)
	assert.Nil(t, distCfg)
}
