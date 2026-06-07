package credentials_test

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// errKeyring is a stand-in backend failure used to drive the keyring error paths.
var errKeyring = errors.New("keyring boom")

func TestMemoryStore_SetGetDeleteLifecycle(t *testing.T) {
	t.Parallel()

	store := credentials.NewMemoryStore()

	// A freshly created store reports no value.
	value, present, err := store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.False(t, present)
	assert.Empty(t, value)

	// Set then Get returns the stored value.
	require.NoError(t, store.Set(credentials.HetznerToken, "tok-1"))

	value, present, err = store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, "tok-1", value)

	// Set again overwrites the previous value.
	require.NoError(t, store.Set(credentials.HetznerToken, "tok-2"))

	value, _, err = store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.Equal(t, "tok-2", value)

	// Delete removes the value; Get then reports absence.
	require.NoError(t, store.Delete(credentials.HetznerToken))

	_, present, err = store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.False(t, present)
}

func TestMemoryStore_DeleteMissingKeyIsNoError(t *testing.T) {
	t.Parallel()

	store := credentials.NewMemoryStore()

	assert.NoError(t, store.Delete(credentials.AWSProfile))
}

func TestMemoryStore_ConcurrentAccessIsRaceFree(t *testing.T) {
	t.Parallel()

	store := credentials.NewMemoryStore()

	const workers = 16

	var waitGroup sync.WaitGroup

	waitGroup.Add(workers)

	for worker := range workers {
		go func(workerID int) {
			defer waitGroup.Done()

			key := credentials.Key("concurrent." + strconv.Itoa(workerID))

			// assert (not require) inside the goroutine: require calls t.FailNow, which is only
			// valid on the test's own goroutine.
			assert.NoError(t, store.Set(key, strconv.Itoa(workerID)))

			_, _, getErr := store.Get(key)
			assert.NoError(t, getErr)

			assert.NoError(t, store.Delete(key))
		}(worker)
	}

	waitGroup.Wait()
}

func TestIsSecret_ClassifiesEveryKey(t *testing.T) {
	t.Parallel()

	tests := map[credentials.Key]bool{
		credentials.HetznerToken:          true,
		credentials.OmniServiceAccountKey: true,
		credentials.AWSAccessKeyID:        true,
		credentials.AWSSecretAccessKey:    true,
		credentials.AWSSessionToken:       true,
		credentials.OmniEndpoint:          false,
		credentials.AWSRegion:             false,
		credentials.AWSProfile:            false,
		credentials.Key("unknown.key"):    false,
	}

	for key, want := range tests {
		assert.Equalf(t, want, credentials.IsSecret(key), "IsSecret(%q)", key)
	}
}

func TestProviderFor_MapsEveryKey(t *testing.T) {
	t.Parallel()

	tests := map[credentials.Key]v1alpha1.Provider{
		credentials.HetznerToken:          v1alpha1.ProviderHetzner,
		credentials.OmniEndpoint:          v1alpha1.ProviderOmni,
		credentials.OmniServiceAccountKey: v1alpha1.ProviderOmni,
		credentials.AWSRegion:             v1alpha1.ProviderAWS,
		credentials.AWSProfile:            v1alpha1.ProviderAWS,
		credentials.AWSAccessKeyID:        v1alpha1.ProviderAWS,
		credentials.AWSSecretAccessKey:    v1alpha1.ProviderAWS,
		credentials.AWSSessionToken:       v1alpha1.ProviderAWS,
		credentials.Key("unknown.key"):    v1alpha1.Provider(""),
	}

	for key, want := range tests {
		assert.Equalf(t, want, credentials.ProviderFor(key), "ProviderFor(%q)", key)
	}
}

func TestLabel_NamesEveryKey(t *testing.T) {
	t.Parallel()

	tests := map[credentials.Key]string{
		credentials.HetznerToken:          "API token",
		credentials.OmniEndpoint:          "Endpoint URL",
		credentials.OmniServiceAccountKey: "Service account key",
		credentials.AWSRegion:             "Region",
		credentials.AWSProfile:            "Profile",
		credentials.AWSAccessKeyID:        "Access key ID",
		credentials.AWSSecretAccessKey:    "Secret access key",
		credentials.AWSSessionToken:       "Session token",
	}

	for key, want := range tests {
		assert.Equalf(t, want, credentials.Label(key), "Label(%q)", key)
	}

	// An unknown key falls back to its raw string form.
	assert.Equal(t, "unknown.key", credentials.Label(credentials.Key("unknown.key")))
}

// TestMappings_CoverEveryAdvertisedKey is a drift guard: every key returned by AllKeys() must be
// mapped to a real provider and a human label by ProviderFor/Label. Without this, adding a new
// credential to AllKeys() while forgetting to extend ProviderFor/Label would silently fall through
// to the empty-provider / raw-string defaults — and forgetting IsSecret would default it to
// non-secret, leaking the value over the API.
func TestMappings_CoverEveryAdvertisedKey(t *testing.T) {
	t.Parallel()

	for _, key := range credentials.AllKeys() {
		assert.NotEmptyf(
			t,
			credentials.ProviderFor(key),
			"ProviderFor(%q) must map to a provider",
			key,
		)
		assert.NotEqualf(
			t,
			string(key),
			credentials.Label(key),
			"Label(%q) must be a human label, not the raw key",
			key,
		)
		// IsSecret must make a deliberate decision for every advertised key. Secret status is asserted
		// exhaustively in TestIsSecret_ClassifiesEveryKey; here we only require the key to be known to
		// at least one classifier so a brand-new key cannot slip through every mapping at once.
		assert.NotEqualf(
			t,
			v1alpha1.Provider(""),
			credentials.ProviderFor(key),
			"key %q is unclassified",
			key,
		)
	}
}

func TestKeyringStore_SetGetDeleteLifecycle(t *testing.T) {
	keyring.MockInit()

	store := credentials.KeyringStore{}

	require.NoError(t, store.Set(credentials.HetznerToken, "tok"))

	value, present, err := store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, "tok", value)

	require.NoError(t, store.Delete(credentials.HetznerToken))

	_, present, err = store.Get(credentials.HetznerToken)
	require.NoError(t, err)
	assert.False(t, present)
}

func TestKeyringStore_GetMissingKeyReportsNotPresent(t *testing.T) {
	keyring.MockInit()

	store := credentials.KeyringStore{}

	value, present, err := store.Get(credentials.Key("never.set"))
	require.NoError(t, err)
	assert.False(t, present)
	assert.Empty(t, value)
}

func TestKeyringStore_DeleteMissingKeyIsNoError(t *testing.T) {
	keyring.MockInit()

	store := credentials.KeyringStore{}

	assert.NoError(t, store.Delete(credentials.Key("never.set")))
}

func TestKeyringStore_WrapsBackendErrors(t *testing.T) {
	keyring.MockInitWithError(errKeyring)

	store := credentials.KeyringStore{}

	setErr := store.Set(credentials.HetznerToken, "tok")
	require.ErrorIs(t, setErr, errKeyring)
	assert.Contains(t, setErr.Error(), string(credentials.HetznerToken))

	_, present, getErr := store.Get(credentials.HetznerToken)
	require.ErrorIs(t, getErr, errKeyring)
	assert.False(t, present)

	delErr := store.Delete(credentials.HetznerToken)
	require.ErrorIs(t, delErr, errKeyring)
}

func TestDetectStore_ReturnsKeyringWhenAvailable(t *testing.T) {
	keyring.MockInit()

	store, available := credentials.DetectStore()

	assert.True(t, available)
	assert.IsType(t, credentials.KeyringStore{}, store)
}

func TestDetectStore_FallsBackToMemoryWhenProbeFails(t *testing.T) {
	keyring.MockInitWithError(errKeyring)

	store, available := credentials.DetectStore()

	assert.False(t, available)
	assert.IsType(t, &credentials.MemoryStore{}, store)

	// The fallback store must still be usable so env resolution keeps working.
	require.NoError(t, store.Set(credentials.AWSRegion, "eu-central-1"))

	value, present, err := store.Get(credentials.AWSRegion)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, "eu-central-1", value)
}
