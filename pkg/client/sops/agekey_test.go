package sops_test

import (
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestValidateAgeKey(t *testing.T) {
	t.Parallel()

	// Generate a real key for the valid case.
	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	validKey := identity.String()

	tests := []struct {
		name       string
		privateKey string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid age key",
			privateKey: validKey,
			wantErr:    false,
		},
		{
			name:       "empty key",
			privateKey: "",
			wantErr:    true,
			errMsg:     "key is empty",
		},
		{
			name:       "whitespace only key",
			privateKey: "   ",
			wantErr:    true,
			errMsg:     "key is empty",
		},
		{
			name:       "wrong prefix",
			privateKey: "NOT-A-VALID-KEY-" + strings.Repeat("x", 60),
			wantErr:    true,
			errMsg:     "key must start with AGE-SECRET-KEY-",
		},
		{
			name:       "correct prefix but too short",
			privateKey: "AGE-SECRET-KEY-SHORT",
			wantErr:    true,
			errMsg:     "key is too short",
		},
		{
			name:       "key with leading/trailing whitespace is trimmed",
			privateKey: "  " + validKey + "  ",
			wantErr:    false,
		},
	}

	for _, tc := range tests { //nolint:varnamelen
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := sopsclient.ValidateAgeKey(tc.privateKey)

			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, sopsclient.ErrInvalidAgeKey)

				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDerivePublicKey(t *testing.T) {
	t.Parallel()

	t.Run("valid private key returns public key", func(t *testing.T) {
		t.Parallel()

		identity, err := age.GenerateX25519Identity()
		require.NoError(t, err)

		publicKey, err := sopsclient.DerivePublicKey(identity.String())

		require.NoError(t, err)
		assert.Equal(t, identity.Recipient().String(), publicKey)
		assert.True(t, strings.HasPrefix(publicKey, "age1"))
	})

	t.Run("private key with whitespace is trimmed", func(t *testing.T) {
		t.Parallel()

		identity, err := age.GenerateX25519Identity()
		require.NoError(t, err)

		publicKey, err := sopsclient.DerivePublicKey("  " + identity.String() + "  \n")

		require.NoError(t, err)
		assert.Equal(t, identity.Recipient().String(), publicKey)
	})

	t.Run("invalid private key returns error", func(t *testing.T) {
		t.Parallel()

		_, err := sopsclient.DerivePublicKey("not-a-valid-key")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse private key")
	})

	t.Run("empty private key returns error", func(t *testing.T) {
		t.Parallel()

		_, err := sopsclient.DerivePublicKey("")

		require.Error(t, err)
	})
}

func TestFormatAgeKeyWithMetadata(t *testing.T) {
	t.Parallel()

	t.Run("with public key", func(t *testing.T) {
		t.Parallel()

		privateKey := "AGE-SECRET-KEY-TESTPRIVATEKEYVALUE"
		publicKey := "age1testpublickeyvalue"

		result := sopsclient.FormatAgeKeyWithMetadata(privateKey, publicKey)

		assert.Contains(t, result, "# created: ")
		assert.Contains(t, result, "# public key: "+publicKey)
		assert.Contains(t, result, privateKey)
		assert.True(t, strings.HasSuffix(result, "\n"), "should end with newline")

		// Verify the timestamp is a valid RFC3339 time.
		lines := strings.Split(result, "\n")
		require.GreaterOrEqual(t, len(lines), 3)

		createdLine := strings.TrimPrefix(lines[0], "# created: ")
		_, err := time.Parse(time.RFC3339, createdLine)
		require.NoError(t, err, "timestamp should be valid RFC3339")
	})

	t.Run("without public key", func(t *testing.T) {
		t.Parallel()

		privateKey := "AGE-SECRET-KEY-TESTPRIVATEKEYVALUE"

		result := sopsclient.FormatAgeKeyWithMetadata(privateKey, "")

		assert.Contains(t, result, "# created: ")
		assert.NotContains(t, result, "# public key:")
		assert.Contains(t, result, privateKey)
		assert.True(t, strings.HasSuffix(result, "\n"))
	})

	t.Run("private key already has trailing newline", func(t *testing.T) {
		t.Parallel()

		privateKey := "AGE-SECRET-KEY-TESTPRIVATEKEYVALUE\n"

		result := sopsclient.FormatAgeKeyWithMetadata(privateKey, "")

		// Should not have double newlines at the end.
		assert.False(
			t,
			strings.HasSuffix(result, "\n\n"),
			"should not have double trailing newline",
		)
		assert.True(t, strings.HasSuffix(result, "\n"))
	})
}
