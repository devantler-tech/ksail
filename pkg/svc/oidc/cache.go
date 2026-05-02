package oidc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

const (
	// cacheDirPerm is the permission for the cache directory.
	cacheDirPerm = 0o700
	// cacheFilePerm is the permission for cache files.
	cacheFilePerm = 0o600
)

// CacheDir returns the default token cache directory.
// Returns an error if the user's home directory cannot be determined.
func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}

	return filepath.Join(home, ".ksail", "oidc", "cache"), nil
}

// CacheKey generates a deterministic cache key from the OIDC parameters.
// Scopes are sorted to ensure consistent keys regardless of input order.
// Fields are separated with null bytes to prevent collision between adjacent values.
func CacheKey(issuerURL, clientID string, scopes []string) string {
	sorted := make([]string, len(scopes))
	copy(sorted, scopes)
	sort.Strings(sorted)

	hasher := sha256.New()
	hasher.Write([]byte(issuerURL))
	hasher.Write([]byte{0})
	hasher.Write([]byte(clientID))

	for _, scope := range sorted {
		hasher.Write([]byte{0})
		hasher.Write([]byte(scope))
	}

	return hex.EncodeToString(hasher.Sum(nil))
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
	cachePath := filepath.Join(cacheDir, key+".json")

	data, err := os.ReadFile(cachePath) //nolint:gosec // G304: cache dir is under ~/.ksail
	if err != nil {
		return nil
	}

	var cached CachedToken

	err = json.Unmarshal(data, &cached)
	if err != nil {
		return nil
	}

	return &cached
}

// SaveCachedToken persists a token result to the cache directory.
func SaveCachedToken(cacheDir, key string, token *TokenResult) error {
	err := os.MkdirAll(cacheDir, cacheDirPerm)
	if err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	cached := CachedToken{
		IDToken:      token.IDToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	data, err := json.MarshalIndent( //nolint:gosec // G117: token caching is the purpose of this file
		cached,
		"",
		"  ",
	)
	if err != nil {
		return fmt.Errorf("failed to marshal cached token: %w", err)
	}

	cachePath := filepath.Join(cacheDir, key+".json")

	writeErr := atomicWriteFile(cachePath, data, cacheFilePerm)
	if writeErr != nil {
		return fmt.Errorf("failed to write cache file: %w", writeErr)
	}

	return nil
}

// atomicWriteFile writes data to a temp file in the same directory and
// renames it to the target path, ensuring an all-or-nothing write.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".oidc-cache-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if chmodErr := os.Chmod(tmpPath, perm); chmodErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("set permissions: %w", chmodErr)
	}

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("write data: %w", writeErr)
	}

	if closeErr := tmp.Close(); closeErr != nil {
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	renameErr := os.Rename(tmpPath, path)
	if renameErr != nil && runtime.GOOS == "windows" {
		if _, statErr := os.Stat(path); statErr == nil {
			_ = os.Remove(path)

			renameErr = os.Rename(tmpPath, path)
		}
	}

	if renameErr != nil {
		return fmt.Errorf("rename temp file: %w", renameErr)
	}

	return nil
}
