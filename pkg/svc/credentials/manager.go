package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
)

// settingsFileMode and settingsDirMode keep the on-disk settings (env-var-name overrides — not
// secrets) private to the user.
const (
	settingsFileMode = 0o600
	settingsDirMode  = 0o700
	settingsFileName = "ui-settings.json"
	ksailDir         = ".ksail"
)

// envVarNamePattern validates POSIX-style environment variable names so a bad override cannot be
// persisted (and later os.Setenv'd).
var envVarNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ErrInvalidEnvVarName indicates a configured environment-variable name is not a valid identifier.
var ErrInvalidEnvVarName = errors.New("invalid environment variable name")

// ErrUnknownCredential indicates an update referenced a credential key KSail does not recognize.
var ErrUnknownCredential = errors.New("unknown credential")

// settings holds the non-secret, file-persisted configuration: per-credential overrides of the
// environment-variable name a credential resolves from. Secret values live in the Store, never here.
type settings struct {
	EnvVars map[Key]string `json:"envVars,omitempty"`
}

// Manager ties the secure Store together with the env-var-name settings file. It is the single
// object the local UI backend uses to resolve, overlay, inspect, and update provider credentials.
// It implements Resolver. Safe for concurrent use.
type Manager struct {
	store    Store
	mu       sync.RWMutex
	settings settings
	// exported tracks the environment-variable names this Manager has set via Overlay, so it can
	// unset stale ones (cleared secrets, renamed variables) without touching variables inherited
	// from the shell.
	exported map[string]struct{}
}

// NewManager loads the settings file and returns a Manager backed by store. A missing settings file
// is treated as empty (first run).
func NewManager(store Store) (*Manager, error) {
	loaded, err := loadSettings()
	if err != nil {
		return nil, err
	}

	return &Manager{store: store, settings: loaded, exported: map[string]struct{}{}}, nil
}

// EnvVar returns the environment-variable name key resolves from: the configured override, or the
// conventional default.
func (m *Manager) EnvVar(key Key) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if name := m.settings.EnvVars[key]; name != "" {
		return name
	}

	return DefaultEnvVar(key)
}

// Value returns the resolved value for key: a stored secure-store value when present, otherwise the
// process-environment value for the configured variable name. "" when unset.
func (m *Manager) Value(key Key) string {
	value, ok, err := m.store.Get(key)
	if err == nil && ok && value != "" {
		return value
	}

	name := m.EnvVar(key)
	if name == "" {
		return ""
	}

	return os.Getenv(name)
}

// Overlay reconciles the process environment to match the stored credentials: it exports every
// stored value under its configured variable name and unsets any variable it previously exported
// that is no longer backed by a stored value (a cleared secret) or whose variable name changed. It
// only touches variables it set itself, never ones inherited from the shell. This makes secure-store
// overrides visible to provider factories and subprocesses (e.g. eksctl) — notably for a desktop app
// launched from the Dock/Finder, which does not inherit the shell environment. Call it at startup
// and after each update.
func (m *Manager) Overlay() error {
	desired := make(map[string]string)

	for _, key := range AllKeys() {
		value, ok, err := m.store.Get(key)
		if err != nil {
			return fmt.Errorf("read %q from store: %w", key, err)
		}

		if !ok || value == "" {
			continue
		}

		// Export under the configured variable name (what discovery resolves) and also under the
		// provider's default name. The create path builds provider specs with the default *EnvVar
		// fields and eksctl reads AWS_REGION directly, so exporting under the default too keeps a
		// stored value usable for creation even when a custom variable name is configured.
		desired[m.EnvVar(key)] = value
		desired[DefaultEnvVar(key)] = value
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Drop variables we exported on a previous Overlay that are no longer desired.
	for name := range m.exported {
		if _, keep := desired[name]; !keep {
			_ = os.Unsetenv(name)
		}
	}

	m.exported = make(map[string]struct{}, len(desired))

	for name, value := range desired {
		setErr := os.Setenv(name, value)
		if setErr != nil {
			return fmt.Errorf("export %q to environment: %w", name, setErr)
		}

		m.exported[name] = struct{}{}
	}

	return nil
}

// CredentialStatus describes a single credential for the Settings UI. It never carries a secret
// value (Value is populated only for non-secret credentials).
type CredentialStatus struct {
	Key    Key
	EnvVar string
	Secret bool
	Stored bool
	Source string // "store", "env", or "unset"
	Value  string // resolved value, for non-secret credentials only
}

// Status reports every credential's current configuration and resolution source.
func (m *Manager) Status() ([]CredentialStatus, error) {
	out := make([]CredentialStatus, 0, len(AllKeys()))

	for _, key := range AllKeys() {
		stored, hasStored, err := m.store.Get(key)
		if err != nil {
			return nil, fmt.Errorf("read %q from store: %w", key, err)
		}

		hasStored = hasStored && stored != ""
		envVar := m.EnvVar(key)
		envValue := os.Getenv(envVar)

		status := CredentialStatus{
			Key:    key,
			EnvVar: envVar,
			Secret: IsSecret(key),
			Stored: hasStored,
			Source: resolutionSource(hasStored, envValue),
		}

		if !status.Secret {
			status.Value = nonSecretValue(hasStored, stored, envValue)
		}

		out = append(out, status)
	}

	return out, nil
}

func resolutionSource(hasStored bool, envValue string) string {
	switch {
	case hasStored:
		return "store"
	case envValue != "":
		return "env"
	default:
		return "unset"
	}
}

func nonSecretValue(hasStored bool, stored, envValue string) string {
	if hasStored {
		return stored
	}

	return envValue
}

// CredentialUpdate mutates one credential. EnvVar nil leaves the name unchanged; "" resets it to the
// default. Value nil leaves the stored value unchanged; "" clears it; any other value stores it.
type CredentialUpdate struct {
	Key    Key
	EnvVar *string
	Value  *string
}

// Update applies env-var-name and stored-value changes, persists the settings file, writes secret
// changes to the store, and re-runs Overlay so the changes take effect without a restart. Updates
// referencing unknown credential keys are rejected so junk is never persisted to disk or the store.
func (m *Manager) Update(updates []CredentialUpdate) error {
	for _, update := range updates {
		if !slices.Contains(AllKeys(), update.Key) {
			return fmt.Errorf("%w: %q", ErrUnknownCredential, update.Key)
		}
	}

	err := m.applyEnvVarOverrides(updates)
	if err != nil {
		return err
	}

	for _, update := range updates {
		if update.Value == nil {
			continue
		}

		if *update.Value == "" {
			delErr := m.store.Delete(update.Key)
			if delErr != nil {
				return fmt.Errorf("clear %q: %w", update.Key, delErr)
			}

			continue
		}

		setErr := m.store.Set(update.Key, *update.Value)
		if setErr != nil {
			return fmt.Errorf("store %q: %w", update.Key, setErr)
		}
	}

	return m.Overlay()
}

func (m *Manager) applyEnvVarOverrides(updates []CredentialUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, update := range updates {
		if update.EnvVar == nil {
			continue
		}

		name := *update.EnvVar
		if name != "" && !envVarNamePattern.MatchString(name) {
			return fmt.Errorf("%w: %q", ErrInvalidEnvVarName, name)
		}

		if m.settings.EnvVars == nil {
			m.settings.EnvVars = map[Key]string{}
		}

		// Empty or default resets to the conventional variable (drops the override).
		if name == "" || name == DefaultEnvVar(update.Key) {
			delete(m.settings.EnvVars, update.Key)
		} else {
			m.settings.EnvVars[update.Key] = name
		}
	}

	return saveSettings(m.settings)
}

// Ensure Manager satisfies Resolver.
var _ Resolver = (*Manager)(nil)

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ksailDir, settingsFileName), nil
}

func loadSettings() (settings, error) {
	path, err := settingsPath()
	if err != nil {
		return settings{}, err
	}

	//nolint:gosec // path is under the user's home directory, derived from a fixed file name.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings{}, nil
	}

	if err != nil {
		return settings{}, fmt.Errorf("read settings: %w", err)
	}

	var loaded settings

	unmarshalErr := json.Unmarshal(data, &loaded)
	if unmarshalErr != nil {
		return settings{}, fmt.Errorf("parse settings: %w", unmarshalErr)
	}

	return loaded, nil
}

func saveSettings(value settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}

	mkErr := os.MkdirAll(filepath.Dir(path), settingsDirMode)
	if mkErr != nil {
		return fmt.Errorf("create settings directory: %w", mkErr)
	}

	data, marshalErr := json.MarshalIndent(value, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("encode settings: %w", marshalErr)
	}

	writeErr := os.WriteFile(path, data, settingsFileMode)
	if writeErr != nil {
		return fmt.Errorf("write settings: %w", writeErr)
	}

	return nil
}
