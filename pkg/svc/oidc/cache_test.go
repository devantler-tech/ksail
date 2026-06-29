package oidc_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheKeyFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		issuerURL string
		clientID  string
		scopes    []string
	}{
		{
			name:      "basic",
			issuerURL: "https://dex.example.com",
			clientID:  "kubectl",
			scopes:    []string{"email"},
		},
		{
			name:      "empty scopes",
			issuerURL: "https://dex.example.com",
			clientID:  "kubectl",
			scopes:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key := oidc.CacheKey(tc.issuerURL, tc.clientID, tc.scopes)
			assert.NotEmpty(t, key)
			assert.Len(t, key, 64) // SHA-256 hex
		})
	}
}

func TestCacheKeyDeterminism(t *testing.T) {
	t.Parallel()

	baseKey := oidc.CacheKey("https://dex.example.com", "kubectl", []string{"email", "groups"})

	t.Run("scope order invariance", func(t *testing.T) {
		t.Parallel()

		reversed := oidc.CacheKey("https://dex.example.com", "kubectl", []string{"groups", "email"})
		assert.Equal(t, baseKey, reversed)
	})

	t.Run("different issuer differs", func(t *testing.T) {
		t.Parallel()

		other := oidc.CacheKey("https://other.example.com", "kubectl", []string{"email", "groups"})
		assert.NotEqual(t, baseKey, other)
	})

	t.Run("different clientID differs", func(t *testing.T) {
		t.Parallel()

		other := oidc.CacheKey("https://dex.example.com", "other", []string{"email", "groups"})
		assert.NotEqual(t, baseKey, other)
	})
}

// TestCacheFederationSharedSession pins the multi-cluster single-sign-on
// guarantee end to end: a token cached while authenticating against one cluster
// is transparently reused by a different cluster that trusts the same OIDC
// provider. The cache key is derived solely from the provider parameters
// (issuer, client ID, scopes) and carries no cluster dimension, so federated
// clusters share one authenticated session — authenticate once, operate across
// the fleet (https://github.com/devantler-tech/ksail/issues/4602).
func TestCacheFederationSharedSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const (
		issuer   = "https://dex.example.com"
		clientID = "kubectl"
	)

	scopes := []string{"email", "groups"}

	// Cluster A authenticates and caches its session.
	clusterAKey := oidc.CacheKey(issuer, clientID, scopes)
	expiry := time.Now().Add(time.Hour).Truncate(time.Second)
	tok := &oidc.TokenResult{IDToken: "shared-id", RefreshToken: "shared-ref", Expiry: expiry}
	require.NoError(t, oidc.SaveCachedToken(dir, clusterAKey, tok))

	// Cluster B trusts the same provider (identical issuer/client/scopes) and
	// therefore resolves the same key — it must find cluster A's session.
	clusterBKey := oidc.CacheKey(issuer, clientID, scopes)
	require.Equal(t, clusterAKey, clusterBKey,
		"clusters sharing an OIDC provider must resolve the same cache key")

	loaded := oidc.LoadCachedToken(dir, clusterBKey)
	require.NotNil(t, loaded, "cluster B must reuse the session cached by cluster A (SSO)")
	assert.Equal(t, "shared-id", loaded.IDToken)
}

func TestLoadSaveCachedToken(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for missing cache file", func(t *testing.T) {
		t.Parallel()
		got := oidc.LoadCachedToken(t.TempDir(), "nonexistent")
		assert.Nil(t, got)
	})

	t.Run("round-trip save and load", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		key := "test-key"
		expiry := time.Now().Add(time.Hour).Truncate(time.Second)
		tok := &oidc.TokenResult{
			IDToken:      "id-tok",
			RefreshToken: "ref-tok",
			Expiry:       expiry,
		}
		require.NoError(t, oidc.SaveCachedToken(dir, key, tok))

		loaded := oidc.LoadCachedToken(dir, key)
		require.NotNil(t, loaded)
		assert.Equal(t, tok.IDToken, loaded.IDToken)
		assert.Equal(t, tok.RefreshToken, loaded.RefreshToken)
		assert.True(t, loaded.Expiry.Equal(expiry))
	})

	t.Run("cache file has restricted permissions", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		tok := &oidc.TokenResult{IDToken: "tok", Expiry: time.Now()}
		require.NoError(t, oidc.SaveCachedToken(dir, "perms", tok))

		info, err := os.Stat(filepath.Join(dir, "perms.json"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("returns nil for corrupt cache file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o600))
		assert.Nil(t, oidc.LoadCachedToken(dir, "bad"))
	})
}
