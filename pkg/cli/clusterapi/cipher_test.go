package clusterapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAgeKey generates an age identity, writes it to a temp keys.txt, points SOPS_AGE_KEY_FILE at
// it (so both recipient discovery and the decrypt keyservice find it), and returns the recipient.
func writeAgeKey(t *testing.T) string {
	t.Helper()

	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	keysPath := filepath.Join(t.TempDir(), "keys.txt")
	require.NoError(t, os.WriteFile(keysPath, []byte(identity.String()+"\n"), 0o600))
	t.Setenv("SOPS_AGE_KEY_FILE", keysPath)

	return identity.Recipient().String()
}

func TestCipherRoundTrip(t *testing.T) { //nolint:paralleltest // t.Setenv
	// Not parallel: t.Setenv (SOPS_AGE_KEY_FILE) is incompatible with t.Parallel.
	recipient := writeAgeKey(t)
	service := clusterapi.NewTestService(nil)
	ctx := context.Background()

	encrypted, err := service.EncryptSecret(ctx, "password: s3cret\n", recipient, "yaml")
	require.NoError(t, err)
	assert.Contains(t, encrypted, "sops")      // SOPS metadata is present
	assert.NotContains(t, encrypted, "s3cret") // the value is encrypted, not plaintext

	decrypted, err := service.DecryptSecret(ctx, encrypted, "yaml")
	require.NoError(t, err)
	assert.Contains(t, decrypted, "password: s3cret")
}

func TestCipherRoundTripDefaultRecipient(t *testing.T) { //nolint:paralleltest // t.Setenv
	// An empty recipient auto-discovers the local age key.
	writeAgeKey(t)

	service := clusterapi.NewTestService(nil)
	ctx := context.Background()

	encrypted, err := service.EncryptSecret(ctx, "token: abc123\n", "", "yaml")
	require.NoError(t, err)

	decrypted, err := service.DecryptSecret(ctx, encrypted, "yaml")
	require.NoError(t, err)
	assert.Contains(t, decrypted, "token: abc123")
}

func TestDecryptSecretRejectsNonAgeSopsMetadata(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	encrypted := `data: ENC[AES256_GCM,data:abc,iv:def,tag:ghi,type:str]
sops:
  hc_vault:
    - vault_address: http://127.0.0.1:1
      engine_path: transit
      key_name: attacker
      created_at: "2026-07-12T00:00:00Z"
      enc: vault:v1:attacker-controlled
  lastmodified: "2026-07-12T00:00:00Z"
  mac: ENC[AES256_GCM,data:abc,iv:def,tag:ghi,type:str]
  version: 3.13.2
`

	_, err := service.DecryptSecret(context.Background(), encrypted, "yaml")

	require.ErrorIs(t, err, api.ErrInvalid)
	assert.ErrorContains(t, err, "only age recipients")
}

func TestCipherRecipientsFromKeyFile(t *testing.T) { //nolint:paralleltest // t.Setenv
	recipient := writeAgeKey(t)
	service := clusterapi.NewTestService(nil)

	recipients, err := service.CipherRecipients(context.Background())
	require.NoError(t, err)
	require.Len(t, recipients, 1)
	assert.Equal(t, recipient, recipients[0])
}

func TestEncryptSecretRejectsEmptyPlaintext(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)

	_, err := service.EncryptSecret(context.Background(), "   ", "age1example", "yaml")
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestEncryptSecretRejectsInvalidRecipient(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)

	_, err := service.EncryptSecret(context.Background(), "k: v\n", "not-an-age-key", "yaml")
	require.ErrorIs(t, err, api.ErrInvalid)
}
