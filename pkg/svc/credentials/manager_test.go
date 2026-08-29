package credentials_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
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

// TestManager_OverlayRestoresInheritedValueOnClear verifies that when a stored override is layered on
// top of a variable inherited from the shell, clearing the override RESTORES the inherited value
// rather than erasing it — preserving the documented store -> os.Getenv resolution order.
func TestManager_OverlayRestoresInheritedValueOnClear(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	envVar := credentials.DefaultEnvVar(credentials.HetznerToken)
	t.Setenv(envVar, "from-shell")

	manager, _ := newManager(t)

	// Storing a value overlays it on top of the inherited shell value.
	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, Value: new("from-store")},
	}))
	assert.Equal(t, "from-store", os.Getenv(envVar))

	// Clearing the stored value must restore the inherited shell value, not unset the variable.
	require.NoError(t, manager.Update([]credentials.CredentialUpdate{
		{Key: credentials.HetznerToken, Value: new("")},
	}))
	assert.Equal(t, "from-shell", os.Getenv(envVar),
		"clearing a stored override must fall back to the inherited environment value")
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

//nolint:gosec // APIKeyEnvVar is an environment-variable name, not a credential.
func TestManager_ChatProviderSettingsPersistAcrossInstances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	first, err := credentials.NewManager(credentials.NewMemoryStore())
	require.NoError(t, err)
	require.NoError(t, first.UpdateAppSettings(credentials.AppSettings{
		ChatProvider:        v1alpha1.AIProviderAzureOpenAI,
		ChatModel:           "production-deployment",
		ChatReasoningEffort: "high",
		ChatBaseURL:         "https://resource.openai.azure.com",
		ChatAPIKeyEnvVar:    "TEAM_AZURE_KEY",
		ChatWireAPI:         "responses",
		ChatAzureAPIVersion: "2025-04-01-preview",
	}))

	second, err := credentials.NewManager(credentials.NewMemoryStore())
	require.NoError(t, err)
	assert.Equal(t, credentials.AppSettings{
		ChatProvider:        v1alpha1.AIProviderAzureOpenAI,
		ChatModel:           "production-deployment",
		ChatReasoningEffort: "high",
		ChatBaseURL:         "https://resource.openai.azure.com",
		ChatAPIKeyEnvVar:    "TEAM_AZURE_KEY",
		ChatWireAPI:         "responses",
		ChatAzureAPIVersion: "2025-04-01-preview",
	}, second.AppSettings())
}

func TestManager_UpdateAppSettingsRejectsInvalidProviderFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	manager, _ := newManager(t)

	err := manager.UpdateAppSettings(credentials.AppSettings{ChatProvider: "unknown"})
	require.ErrorIs(t, err, credentials.ErrInvalidAIProvider)

	err = manager.UpdateAppSettings(credentials.AppSettings{ChatWireAPI: "messages"})
	require.ErrorIs(t, err, credentials.ErrInvalidAIWireAPI)

	err = manager.UpdateAppSettings(credentials.AppSettings{ChatAPIKeyEnvVar: "TEAM=KEY"})
	require.ErrorIs(t, err, credentials.ErrInvalidEnvVarName)
}

// A stored BYOK key must be reachable under the name the chat settings name, because
// chat.ResolveProvider fails closed on an explicit APIKeyEnvVar: when one is configured it consults
// that variable and nothing else. Chat.APIKeyEnvVar is a separate setting from the per-credential
// EnvVars override that Manager.EnvVar reads, so without this export a user who stores a key AND
// names a variable gets "AI provider API key is required" while holding a stored key.
func TestManager_OverlayExportsAIKeyUnderChatConfiguredEnvVar(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TEAM_AI_KEY", "")

	manager, store := newManager(t)

	require.NoError(t, manager.UpdateAppSettings(credentials.AppSettings{
		ChatProvider:     v1alpha1.AIProviderOpenAI,
		ChatModel:        "gpt-5",
		ChatAPIKeyEnvVar: "TEAM_AI_KEY",
	}))
	require.NoError(t, store.Set(credentials.AIProviderAPIKey, "stored-ai-key"))
	require.NoError(t, manager.Overlay())

	assert.Equal(t, "stored-ai-key", os.Getenv("TEAM_AI_KEY"),
		"overlay must export the stored AI key under the chat-configured variable name")
	assert.Equal(t, "stored-ai-key",
		os.Getenv(credentials.DefaultEnvVar(credentials.AIProviderAPIKey)),
		"overlay must still export under the default name")
}

// Overlay's error is logged verbatim by newCredentialManager, and the api-key variable name it used
// to carry comes straight out of ui-settings.json — a file the load path does not validate, unlike
// UpdateAppSettings. Naming the credential instead keeps the diagnostic that matters (which
// credential failed) while keeping unvalidated file content out of the log line.
func TestManager_OverlayErrorNamesCredentialNotConfiguredVariable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Written directly rather than through UpdateAppSettings: that path rejects the name, and the
	// gap under test is precisely that the load path does not.
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ksail"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".ksail", "ui-settings.json"),
		[]byte(`{"chat":{"apiKeyEnvVar":"BAD=NAME"}}`),
		0o600,
	))

	manager, store := newManager(t)
	require.NoError(t, store.Set(credentials.AIProviderAPIKey, "stored-ai-key"))

	err := manager.Overlay()
	require.Error(t, err, "a variable name os.Setenv rejects must surface as an error")
	assert.NotContains(t, err.Error(), "BAD=NAME",
		"the export error must not echo the settings-file variable name into a logged line")
	assert.Contains(t, err.Error(), string(credentials.AIProviderAPIKey),
		"the export error must still name which credential could not be exported")
}
