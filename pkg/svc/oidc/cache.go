package oidc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// cacheDirPerm is the permission for the cache directory.
	cacheDirPerm = 0o700
	// cacheFilePerm is the permission for cache files.
	cacheFilePerm = 0o600
)

// CacheDir returns the default token cache directory.
func CacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".ksail", "oidc", "cache")
}

// CacheKey generates a deterministic cache key from the OIDC parameters.
func CacheKey(issuerURL, clientID string, scopes []string) string {
	h := sha256.New()
	h.Write([]byte(issuerURL))
	h.Write([]byte(clientID))

	for _, s := range scopes {
		h.Write([]byte(s))
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// CachedToken represents a cached OIDC token on disk.
type CachedToken struct {
	IDToken      string    `json:"idToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// LoadCachedToken loads a cached token from disk.
// Returns nil if no cached token exists or it cannot be read.
func LoadCachedToken(cacheDir, key string) *CachedToken {
	path := filepath.Join(cacheDir, key+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var cached CachedToken
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil
	}

	return &cached
}

// SaveCachedToken persists a token result to the cache directory.
func SaveCachedToken(cacheDir, key string, token *TokenResult) error {
	if err := os.MkdirAll(cacheDir, cacheDirPerm); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	cached := CachedToken{
		IDToken:      token.IDToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cached token: %w", err)
	}

	path := filepath.Join(cacheDir, key+".json")

	return os.WriteFile(path, data, cacheFilePerm)
}
