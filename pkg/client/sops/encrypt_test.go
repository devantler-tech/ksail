package sops_test

import (
	"testing"

	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/getsops/sops/v3"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataFromEncryptionConfig(t *testing.T) {
	t.Parallel()

	t.Run("empty config produces default metadata", func(t *testing.T) {
		t.Parallel()

		config := sopsclient.EncryptConfig{}

		metadata := sopsclient.MetadataFromEncryptionConfig(config)

		assert.Empty(t, metadata.UnencryptedSuffix)
		assert.Empty(t, metadata.EncryptedSuffix)
		assert.Empty(t, metadata.UnencryptedRegex)
		assert.Empty(t, metadata.EncryptedRegex)
		assert.Empty(t, metadata.UnencryptedCommentRegex)
		assert.Empty(t, metadata.EncryptedCommentRegex)
		assert.False(t, metadata.MACOnlyEncrypted)
		assert.Equal(t, version.Version, metadata.Version)
		assert.Equal(t, 0, metadata.ShamirThreshold)
		assert.Nil(t, metadata.KeyGroups)
	})

	t.Run("fully populated config", func(t *testing.T) {
		t.Parallel()

		ageKey, err := sopsage.MasterKeyFromRecipient(
			"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p",
		)
		require.NoError(t, err)

		config := sopsclient.EncryptConfig{
			UnencryptedSuffix:       "_unencrypted",
			EncryptedSuffix:         "_encrypted",
			UnencryptedRegex:        "^plain_.*",
			EncryptedRegex:          "^secret_.*",
			UnencryptedCommentRegex: "sops:unencrypted",
			EncryptedCommentRegex:   "sops:encrypted",
			MACOnlyEncrypted:        true,
			KeyGroups:               []sops.KeyGroup{{ageKey}},
			GroupThreshold:          2,
		}

		metadata := sopsclient.MetadataFromEncryptionConfig(config)

		assert.Equal(t, "_unencrypted", metadata.UnencryptedSuffix)
		assert.Equal(t, "_encrypted", metadata.EncryptedSuffix)
		assert.Equal(t, "^plain_.*", metadata.UnencryptedRegex)
		assert.Equal(t, "^secret_.*", metadata.EncryptedRegex)
		assert.Equal(t, "sops:unencrypted", metadata.UnencryptedCommentRegex)
		assert.Equal(t, "sops:encrypted", metadata.EncryptedCommentRegex)
		assert.True(t, metadata.MACOnlyEncrypted)
		assert.Equal(t, version.Version, metadata.Version)
		assert.Equal(t, 2, metadata.ShamirThreshold)
		require.Len(t, metadata.KeyGroups, 1)
		require.Len(t, metadata.KeyGroups[0], 1)
	})
}

func TestFileAlreadyEncryptedError(t *testing.T) {
	t.Parallel()

	err := &sopsclient.FileAlreadyEncryptedError{}

	assert.Equal(t, "file already encrypted", err.Error())
	assert.Implements(t, (*error)(nil), err)
}
