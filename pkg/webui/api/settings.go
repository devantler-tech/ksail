package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SettingsService backs the credential-settings endpoints. It is optional: only the local UI
// backend (`ksail open web` / the desktop app) provides one, since it resolves cloud credentials from the
// environment and an OS secure store. The operator manages credentials in-cluster and leaves it
// nil, in which case the settings routes are not registered and the SPA hides the Settings page.
type SettingsService interface {
	// Get returns the current per-credential settings (never including secret values).
	Get(ctx context.Context) (SettingsResponse, error)
	// Update applies the requested changes and returns the resulting settings.
	Update(ctx context.Context, request SettingsUpdateRequest) (SettingsResponse, error)
	// AppSettings returns the local UI app preferences (editor command + chat model/effort).
	AppSettings(ctx context.Context) (AppSettings, error)
	// UpdateAppSettings persists the app preferences and returns the resulting settings.
	UpdateAppSettings(ctx context.Context, request AppSettings) (AppSettings, error)
	// TestConnection checks whether the stored credentials for a provider authenticate. A failed
	// connection is reported via the result (OK=false), not an error; an error is returned only for an
	// unsupported provider so the handler can answer 400.
	TestConnection(ctx context.Context, provider string) (CredentialTestResult, error)
}

// SettingsResponse is the settings payload the SPA renders.
type SettingsResponse struct {
	Credentials []CredentialSetting `json:"credentials"`
	// SecureStorageAvailable is false when no OS secure store (keychain) is reachable. In that mode
	// secret values entered here are held only in memory for the running process and are lost on
	// restart; the SPA surfaces this so it never implies secrets are securely persisted.
	SecureStorageAvailable bool `json:"secureStorageAvailable"`
}

// CredentialSetting describes one provider credential's configuration and resolution source. Secret
// values are never included; only their presence (Stored/Source) is surfaced.
type CredentialSetting struct {
	Key      string `json:"key"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	EnvVar   string `json:"envVar"`
	Secret   bool   `json:"secret"`
	Stored   bool   `json:"stored"`
	Source   string `json:"source"`          // "store", "env", or "unset"
	Value    string `json:"value,omitempty"` // resolved value, non-secret credentials only
}

// SettingsUpdateRequest is the body of PUT /api/v1/settings.
type SettingsUpdateRequest struct {
	Updates []CredentialUpdate `json:"updates"`
}

// CredentialUpdate mutates one credential. A nil pointer leaves that aspect unchanged; for EnvVar an
// empty string resets to the default variable name, and for Value an empty string clears the stored
// secret.
type CredentialUpdate struct {
	Key    string  `json:"key"`
	EnvVar *string `json:"envVar,omitempty"`
	Value  *string `json:"value,omitempty"`
}

// AppSettings is the local UI's non-credential preferences surfaced on the Settings page. It is the
// body of both GET and PUT /api/v1/settings/app (a PUT replaces the stored values).
type AppSettings struct {
	// Editor is the command used for interactive editor flows (e.g. "code --wait").
	Editor string `json:"editor"`
	// Chat holds the AI assistant preferences.
	Chat ChatSettings `json:"chat"`
}

// ChatSettings is the AI assistant's model selection and reasoning effort. Empty values defer to the
// runtime defaults.
type ChatSettings struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort"`
}

// CredentialTestResult reports the outcome of a provider connection test. OK=false with a Message is
// a normal result (the credentials did not authenticate), not a transport error.
type CredentialTestResult struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
}

func (s *Server) handleGetSettings(writer http.ResponseWriter, request *http.Request) {
	response, err := s.Settings.Get(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)

		return
	}

	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handleUpdateSettings(writer http.ResponseWriter, request *http.Request) {
	limited := http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)

	var update SettingsUpdateRequest

	decodeErr := json.NewDecoder(limited).Decode(&update)
	if decodeErr != nil {
		writeDecodeError(writer, fmt.Errorf("decode settings update: %w", decodeErr))

		return
	}

	response, err := s.Settings.Update(request.Context(), update)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handleGetAppSettings(writer http.ResponseWriter, request *http.Request) {
	response, err := s.Settings.AppSettings(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)

		return
	}

	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handleUpdateAppSettings(writer http.ResponseWriter, request *http.Request) {
	limited := http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)

	var update AppSettings

	decodeErr := json.NewDecoder(limited).Decode(&update)
	if decodeErr != nil {
		writeDecodeError(writer, fmt.Errorf("decode app settings: %w", decodeErr))

		return
	}

	response, err := s.Settings.UpdateAppSettings(request.Context(), update)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handleTestCredential(writer http.ResponseWriter, request *http.Request) {
	provider := request.PathValue("provider")

	result, err := s.Settings.TestConnection(request.Context(), provider)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, result)
}
