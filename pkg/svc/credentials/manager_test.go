package credentials_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newManager builds a Manager over an in-memory store. Callers must first point HOME at a temp dir
// (via t.Setenv) so the settings file never touches the developer's real ~/.ksail. Because these
// tests mutate process state (HOME / env vars), they are intentionally not parallel.
func newManager(t *testing.T) (*credentials.Manager, *credentials.MemoryStore) {
	t.Helper()

	store := credentials.NewMemoryStore()

	manager, err := credentials.NewManager(store)
	require.NoError(t, err)

	return manager, store
}

func TestManager_StoreValueOverridesEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, store := newManager(t)

	t.Setenv(credentials.DefaultEnvVar(credentials.HetznerToken), "from-env")
	assert.Equal(t, "from-env", manager.Value(credentials.HetznerToken))

	require.NoError(t, store.Set(credentials.HetznerToken, "from-store"))
	assert.Equal(t, "from-store", manager.Value(credentials.HetznerToken),
		"a stored value must override the environment")
}

func TestManager_ConfigurableEnvVarName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, _ := newManager(t)

	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, EnvVar: new("MY_HCLOUD")},
	}))

	assert.Equal(t, "MY_HCLOUD", manager.EnvVar(credentials.HetznerToken))

	t.Setenv("MY_HCLOUD", "tok")
	assert.Equal(t, "tok", manager.Value(credentials.HetznerToken),
		"value must resolve from the configured variable name")
}

func TestManager_OverlayExportsStoredSecretsUnderConfiguredName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, store := newManager(t)

	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, EnvVar: new("MY_HCLOUD")},
	}))
	require.NoError(t, store.Set(credentials.HetznerToken, "secret-token"))

	require.NoError(t, manager.Overlay())
	assert.Equal(t, "secret-token", os.Getenv("MY_HCLOUD"),
		"overlay must export the stored value under the configured variable name")
	assert.Equal(t, "secret-token", os.Getenv(credentials.DefaultEnvVar(credentials.HetznerToken)),
		"overlay must also export under the default name so the create path/eksctl see it")
}

func TestManager_UpdateStoresAndClearsSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, store := newManager(t)

	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, Value: new("abc")},
	}))
	value, present, err := store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, "abc", value)

	// Empty value clears the stored secret.
	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, Value: new("")},
	}))
	_, present, err = store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.False(t, present)
}

func TestManager_OverlayUnsetsClearedSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	envVar := credentials.DefaultEnvVar(credentials.HetznerToken)
	t.Setenv(envVar, "")

	manager, _ := newManager(t)

	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, Value: new("stored-secret")},
	}))
	assert.Equal(t, "stored-secret", os.Getenv(envVar))

	// Clearing the stored secret must remove it from the process env, not leave it lingering.
	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, Value: new("")},
	}))
	assert.Empty(t, os.Getenv(envVar),
		"clearing a stored secret must unset the previously-exported variable")
}

func TestManager_UpdateRejectsUnknownKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, _ := newManager(t)

	err := manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.Key("nope.nope"), Value: new("x")},
	})
	require.ErrorIs(t, err, credentials.ErrUnknownCredential)
}

func TestManager_UpdateRejectsInvalidEnvVarName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, _ := newManager(t)

	err := manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, EnvVar: new("bad name!")},
	})
	require.ErrorIs(t, err, credentials.ErrInvalidEnvVarName)
}

func TestManager_StatusMasksSecretsButShowsNonSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, store := newManager(t)

	require.NoError(t, store.Set(credentials.HetznerToken, "super-secret"))
	require.NoError(t, store.Set(credentials.AWSRegion, "eu-central-1"))

	statuses, err := manager.Status()
	require.NoError(t, err)

	byKey := map[credentials.Key]credentials.CredentialStatus{}
	for _, status := range statuses {
		byKey[status.Key] = status
	}

	token := byKey[credentials.HetznerToken]
	assert.True(t, token.Secret)
	assert.True(t, token.Stored)
	assert.Equal(t, "store", token.Source)
	assert.Empty(t, token.Value, "secret values must never be surfaced")

	region := byKey[credentials.AWSRegion]
	assert.False(t, region.Secret)
	assert.Equal(t, "eu-central-1", region.Value, "non-secret values may be shown for editing")
}

func TestManager_SettingsPersistAcrossInstances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	first, err := credentials.NewManager(credentials.NewMemoryStore())
	require.NoError(t, err)
	require.NoError(t, first.Update([]credentials.CredentialUpdate{
		{Key: credentials.OmniEndpoint, EnvVar: new("MY_OMNI_ENDPOINT")},
	}))

	// The settings file must exist and be reloaded by a fresh Manager.
	settingsFile := filepath.Join(os.Getenv("HOME"), ".ksail", "ui-settings.json")
	assert.FileExists(t, settingsFile)

	second, err := credentials.NewManager(credentials.NewMemoryStore())
	require.NoError(t, err)
	assert.Equal(t, "MY_OMNI_ENDPOINT", second.EnvVar(credentials.OmniEndpoint))
}
