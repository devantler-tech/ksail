package sops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newValidAgeKey generates a real X25519 age private key for use in import tests.
func newValidAgeKey(t *testing.T) string {
	t.Helper()

	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	return identity.String()
}

// These tests mutate SOPS_AGE_KEY_FILE via t.Setenv, so they cannot run in parallel.

func TestGetAgeKeyPath(t *testing.T) {
	want := filepath.Join(t.TempDir(), "sops", "age", "keys.txt")
	t.Setenv("SOPS_AGE_KEY_FILE", want)

	got, err := sopsclient.GetAgeKeyPath()

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestImportKeyCreatesNewFile(t *testing.T) {
	privateKey := newValidAgeKey(t)
	// Point at a not-yet-existing nested path to exercise MkdirAll and the new-file branch.
	keyPath := filepath.Join(t.TempDir(), "nested", "sops", "age", "keys.txt")
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)

	err := sopsclient.ImportKey(privateKey)
	require.NoError(t, err)

	info, statErr := os.Stat(keyPath)
	require.NoError(t, statErr)
	assert.Equal(t, sopsclient.AgeKeyFilePermissions, info.Mode().Perm())

	content, readErr := os.ReadFile(keyPath) //nolint:gosec // test temp path
	require.NoError(t, readErr)

	text := string(content)
	assert.Contains(t, text, "# created: ")
	assert.Contains(t, text, privateKey)

	publicKey, deriveErr := sopsclient.DerivePublicKey(privateKey)
	require.NoError(t, deriveErr)
	assert.Contains(t, text, "# public key: "+publicKey)
}

func TestImportKeyAppendsToExistingFile(t *testing.T) {
	firstKey := newValidAgeKey(t)
	secondKey := newValidAgeKey(t)
	keyPath := filepath.Join(t.TempDir(), "keys.txt")
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)

	require.NoError(t, sopsclient.ImportKey(firstKey))
	require.NoError(t, sopsclient.ImportKey(secondKey))

	content, err := os.ReadFile(keyPath) //nolint:gosec // test temp path
	require.NoError(t, err)

	text := string(content)
	assert.Contains(t, text, firstKey)
	assert.Contains(t, text, secondKey)
	// Two "# created: " headers prove the second key was appended, not overwritten.
	assert.Equal(t, 2, strings.Count(text, "# created: "))
}

func TestImportKeyRejectsInvalidKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "keys.txt")
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)

	err := sopsclient.ImportKey("not-a-valid-key")
	require.Error(t, err)
	require.ErrorIs(t, err, sopsclient.ErrInvalidAgeKey)

	_, statErr := os.Stat(keyPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "no file should be written for an invalid key")
}

func TestImportKeyRejectsUnparseableKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "keys.txt")
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)

	// Passes validateAgeKey (correct prefix and length) but is not a real X25519 identity,
	// so DerivePublicKey must fail before anything is written.
	bogus := sopsclient.AgeKeyPrefix + strings.Repeat("X", 60)

	err := sopsclient.ImportKey(bogus)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to derive public key")

	_, statErr := os.Stat(keyPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "no file should be written on derive failure")
}

func TestImportKeyReportsDirCreationFailure(t *testing.T) {
	privateKey := newValidAgeKey(t)
	// A regular file stands in for the parent directory, so MkdirAll cannot create it.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), sopsclient.AgeKeyFilePermissions))
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(blocker, "age", "keys.txt"))

	err := sopsclient.ImportKey(privateKey)
	require.Error(t, err)
	require.ErrorIs(t, err, sopsclient.ErrFailedToCreateDir)
}

func TestImportKeyReportsWriteFailure(t *testing.T) {
	privateKey := newValidAgeKey(t)
	// Point the key path at an existing directory: Stat succeeds, but the file cannot be
	// opened for writing, exercising the append-path error branch.
	dir := t.TempDir()
	t.Setenv("SOPS_AGE_KEY_FILE", dir)

	err := sopsclient.ImportKey(privateKey)
	require.Error(t, err)
	require.ErrorIs(t, err, sopsclient.ErrFailedToWriteKey)
}
