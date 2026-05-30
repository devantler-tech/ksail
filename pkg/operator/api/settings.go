package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SettingsService backs the credential-settings endpoints. It is optional: only the local UI
// backend (`ksail ui` / the desktop app) provides one, since it resolves cloud credentials from the
// environment and an OS secure store. The operator manages credentials in-cluster and leaves it
// nil, in which case the settings routes are not registered and the SPA hides the Settings page.
type SettingsService interface {
	// Get returns the current per-credential settings (never including secret values).
	Get(ctx context.Context) (SettingsResponse, error)
	// Update applies the requested changes and returns the resulting settings.
	Update(ctx context.Context, request SettingsUpdateRequest) (SettingsResponse, error)
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
