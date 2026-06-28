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

// ErrInvalidReasoningEffort indicates a chat reasoning-effort value outside the allowed set.
var ErrInvalidReasoningEffort = errors.New("invalid reasoning effort")

// editorEnvVar is the conventional environment variable KSail's editor resolution honors; Overlay
// exports the configured editor command under it so CLI editor flows (and subprocesses) pick it up.
const editorEnvVar = "EDITOR"

// validReasoningEffort reports whether effort is one UpdateAppSettings accepts (matching
// ChatSpec.ReasoningEffort); "" means "leave to the runtime default".
func validReasoningEffort(effort string) bool {
	switch effort {
	case "", "low", "medium", "high":
		return true
	default:
		return false
	}
}

// settings holds the non-secret, file-persisted configuration: per-credential overrides of the
// environment-variable name a credential resolves from, plus local UI app preferences (editor
// command, chat model/effort). Secret values live in the Store, never here.
type settings struct {
	EnvVars map[Key]string `json:"envVars,omitempty"`
	// Editor is the command used for interactive editor flows (e.g. "code --wait"). Exported to the
	// EDITOR environment variable by Overlay so KSail's editor resolution and subprocesses honor it.
	Editor string `json:"editor,omitempty"`
	// Chat holds AI assistant preferences. A pointer so an unset value is omitted from the file.
	Chat *chatPrefs `json:"chat,omitempty"`
}

// chatPrefs holds the AI assistant model selection and reasoning effort.
type chatPrefs struct {
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// AppSettings is the local UI's non-credential preferences (editor command + chat model/effort),
// persisted alongside the env-var overrides in ui-settings.json.
type AppSettings struct {
	Editor              string
	ChatModel           string
	ChatReasoningEffort string
}

// envSnapshot records what a process environment variable held before Overlay first overrode it, so
// the original (e.g. a value inherited from the shell) can be restored when the stored override is
// cleared or its variable name changes — preserving the store → os.Getenv resolution order.
type envSnapshot struct {
	value   string
	present bool
}

// Manager ties the secure Store together with the env-var-name settings file. It is the single
// object the local UI backend uses to resolve, overlay, inspect, and update provider credentials.
// It implements Resolver. Safe for concurrent use.
type Manager struct {
	store    Store
	mu       sync.RWMutex
	settings settings
	// exported maps each environment-variable name this Manager has set via Overlay to the value the
	// variable held *before* the first override. When a name is no longer backed by a stored value
	// (cleared secret or renamed variable) it is restored to that original — re-set if it was present,
	// unset if not — rather than blindly unset, so clearing a keychain override falls back to the
	// inherited shell value instead of erasing it.
	exported map[string]envSnapshot
}

// NewManager loads the settings file and returns a Manager backed by store. A missing settings file
// is treated as empty (first run).
func NewManager(store Store) (*Manager, error) {
	loaded, err := loadSettings()
	if err != nil {
		return nil, err
	}

	return &Manager{store: store, settings: loaded, exported: map[string]envSnapshot{}}, nil
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

	return resolveEnvValue(key, name)
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

	m.addEditorOverlay(desired)

	// Restore variables we exported on a previous Overlay that are no longer desired (cleared secret
	// or renamed variable) to the value they held before we overrode them — re-setting an inherited
	// value rather than erasing it — so resolution falls back to the environment as intended.
	for name, original := range m.exported {
		if _, keep := desired[name]; keep {
			continue
		}

		restoreEnv(name, original)
		delete(m.exported, name)
	}

	for name, value := range desired {
		// Capture the pre-override value the first time we set a given variable so a later clear can
		// restore it. If we already track the name, the current value is our own override from a prior
		// Overlay, not the inherited original — so do not recapture it.
		if _, tracked := m.exported[name]; !tracked {
			original, present := os.LookupEnv(name)
			m.exported[name] = envSnapshot{value: original, present: present}
		}

		setErr := os.Setenv(name, value)
		if setErr != nil {
			return fmt.Errorf("export %q to environment: %w", name, setErr)
		}
	}

	return nil
}

// restoreEnv returns a variable to the snapshot captured before Overlay overrode it: the original
// value when it was present, otherwise unset.
func restoreEnv(name string, original envSnapshot) {
	if original.present {
		_ = os.Setenv(name, original.value)

		return
	}

	_ = os.Unsetenv(name)
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
		envValue := resolveEnvValue(key, envVar)

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

// AppSettings returns the local UI app preferences (editor command + chat model/effort).
func (m *Manager) AppSettings() AppSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.settings.appSettings()
}

// UpdateAppSettings persists the editor command and chat preferences, then re-runs Overlay so an
// editor change takes effect (via EDITOR) without a restart. An invalid reasoning effort is rejected.
func (m *Manager) UpdateAppSettings(next AppSettings) error {
	if !validReasoningEffort(next.ChatReasoningEffort) {
		return fmt.Errorf("%w: %q", ErrInvalidReasoningEffort, next.ChatReasoningEffort)
	}

	m.mu.Lock()

	prevEditor, prevChat := m.settings.Editor, m.settings.Chat
	m.settings.Editor = next.Editor

	if next.ChatModel == "" && next.ChatReasoningEffort == "" {
		m.settings.Chat = nil
	} else {
		m.settings.Chat = &chatPrefs{
			Model:           next.ChatModel,
			ReasoningEffort: next.ChatReasoningEffort,
		}
	}

	saveErr := saveSettings(m.settings)
	if saveErr != nil {
		// Roll back the in-memory mutation so a failed persist doesn't leave rejected values live (a
		// later successful write would otherwise commit them).
		m.settings.Editor, m.settings.Chat = prevEditor, prevChat
		m.mu.Unlock()

		return saveErr
	}

	m.mu.Unlock()

	return m.Overlay()
}

// addEditorOverlay adds the configured editor command to the desired environment under EDITOR so
// KSail's editor resolution (and any editor subprocess) honors it — important for a Dock/Finder-
// launched desktop app with no shell env. The caller must hold m.mu.
func (m *Manager) addEditorOverlay(desired map[string]string) {
	if m.settings.Editor != "" {
		desired[editorEnvVar] = m.settings.Editor
	}
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

// appSettings projects the persisted settings onto the public AppSettings shape.
func (s settings) appSettings() AppSettings {
	out := AppSettings{Editor: s.Editor}
	if s.Chat != nil {
		out.ChatModel = s.Chat.Model
		out.ChatReasoningEffort = s.Chat.ReasoningEffort
	}

	return out
}

// LoadAppSettings reads the app preferences from the settings file directly, returning zero values
// when the file is absent or unreadable. Best-effort: used for non-critical defaults (e.g. seeding
// the web assistant's chat model/effort) without requiring a Manager instance.
func LoadAppSettings() AppSettings {
	loaded, err := loadSettings()
	if err != nil {
		return AppSettings{}
	}

	return loaded.appSettings()
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
