package credentials

import (
	"errors"
	"fmt"
	"sync"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/zalando/go-keyring"
)

// keyringService is the service name KSail's credentials are stored under in the OS secure store.
const keyringService = "ksail"

// Store persists credential VALUES in a secure backend, keyed by credential Key. Implementations
// must be safe for concurrent use.
type Store interface {
	// Get returns the stored value for key and whether one is present.
	Get(key Key) (value string, present bool, err error)
	// Set stores value for key, replacing any existing value.
	Set(key Key, value string) error
	// Delete removes any stored value for key. Deleting a missing key is not an error.
	Delete(key Key) error
}

// KeyringStore persists credentials in the OS secure store (macOS Keychain, Windows Credential
// Manager, Linux secret-service) via go-keyring.
type KeyringStore struct{}

// Get implements Store.
func (KeyringStore) Get(key Key) (string, bool, error) {
	value, err := keyring.Get(keyringService, string(key))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("read %q from keyring: %w", key, err)
	}

	return value, true, nil
}

// Set implements Store.
func (KeyringStore) Set(key Key, value string) error {
	err := keyring.Set(keyringService, string(key), value)
	if err != nil {
		return fmt.Errorf("write %q to keyring: %w", key, err)
	}

	return nil
}

// Delete implements Store.
func (KeyringStore) Delete(key Key) error {
	err := keyring.Delete(keyringService, string(key))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete %q from keyring: %w", key, err)
	}

	return nil
}

// MemoryStore is an in-memory Store for tests and as a fallback when no OS secure store is
// available (e.g. a headless Linux host without secret-service).
type MemoryStore struct {
	mu     sync.RWMutex
	values map[Key]string
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: map[Key]string{}}
}

// Get implements Store.
func (m *MemoryStore) Get(key Key) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.values[key]

	return value, ok, nil
}

// Set implements Store.
func (m *MemoryStore) Set(key Key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.values[key] = value

	return nil
}

// Delete implements Store.
func (m *MemoryStore) Delete(key Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.values, key)

	return nil
}

var (
	_ Store = KeyringStore{}
	_ Store = (*MemoryStore)(nil)
)

// storeProbeName is a throwaway entry written then deleted to detect whether the OS secure store is
// usable. It is a plain string (not a credential Key) so it is not treated as a credential.
const storeProbeName = "ksail.store.probe"

// DetectStore returns a KeyringStore when the OS secure store is usable, otherwise an in-memory
// store (env resolution still works; overrides just are not persisted). The boolean reports whether
// persistence is available, so callers can surface "keychain unavailable" in the UI. The probe is a
// write+delete of a throwaway key, which is the only reliable cross-platform availability check.
func DetectStore() (Store, bool) {
	keyringStore := KeyringStore{}

	err := keyringStore.Set(Key(storeProbeName), "probe")
	if err != nil {
		return NewMemoryStore(), false
	}

	_ = keyringStore.Delete(Key(storeProbeName))

	return keyringStore, true
}

// IsSecret reports whether a credential's value is sensitive and must never be returned over the
// API (only its presence is surfaced). Non-secret values (region, profile, endpoint) may be shown
// so the user can see and edit them.
func IsSecret(key Key) bool {
	switch key {
	case HetznerToken, OmniServiceAccountKey, AWSAccessKeyID, AWSSecretAccessKey, AWSSessionToken,
		CopilotToken:
		return true
	case OmniEndpoint, AWSRegion, AWSProfile, GCPProject, GCPLocation,
		AzureSubscriptionID, AzureResourceGroup:
		return false
	default:
		return false
	}
}

// copilotGroup is the Settings UI section label for the GitHub Copilot credential — a feature group
// rather than an infrastructure provider.
const copilotGroup = "GitHub Copilot"

// ProviderFor returns the Settings UI group a credential belongs to: the infrastructure provider for
// cloud credentials, or a feature group (e.g. the AI assistant) for non-provider credentials.
func ProviderFor(key Key) string {
	switch key {
	case HetznerToken:
		return string(v1alpha1.ProviderHetzner)
	case OmniEndpoint, OmniServiceAccountKey:
		return string(v1alpha1.ProviderOmni)
	case AWSRegion, AWSProfile, AWSAccessKeyID, AWSSecretAccessKey, AWSSessionToken:
		return string(v1alpha1.ProviderAWS)
	case GCPProject, GCPLocation:
		return string(v1alpha1.ProviderGCP)
	case AzureSubscriptionID, AzureResourceGroup:
		return string(v1alpha1.ProviderAzure)
	case CopilotToken:
		return copilotGroup
	default:
		return ""
	}
}

// Label returns a short human-readable name for a credential, for the Settings UI; an unknown key
// falls back to its raw string form.
func Label(key Key) string {
	labels := map[Key]string{
		HetznerToken:          "API token",
		OmniEndpoint:          "Endpoint URL",
		OmniServiceAccountKey: "Service account key",
		AWSRegion:             "Region",
		AWSProfile:            "Profile",
		AWSAccessKeyID:        "Access key ID",
		AWSSecretAccessKey:    "Secret access key",
		AWSSessionToken:       "Session token",
		GCPProject:            "Project ID",
		GCPLocation:           "Location",
		AzureSubscriptionID:   "Subscription ID",
		AzureResourceGroup:    "Resource group",
		CopilotToken:          "Token",
	}

	if label, ok := labels[key]; ok {
		return label
	}

	return string(key)
}
