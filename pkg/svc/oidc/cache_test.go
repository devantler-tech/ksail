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
		{name: "basic", issuerURL: "https://dex.example.com", clientID: "kubectl", scopes: []string{"email"}},
		{name: "empty scopes", issuerURL: "https://dex.example.com", clientID: "kubectl", scopes: nil},
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
