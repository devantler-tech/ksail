package uiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// newCredentialManager builds the credential manager backing the local UI's resolution, env overlay,
// and Settings page. It degrades gracefully: a corrupt settings file yields nil (the caller then
// falls back to plain environment resolution with no Settings page). The returned bool reports
// whether secret values persist in an OS secure store; when false, the Settings page still works but
// must signal that secrets are not securely persisted.
func newCredentialManager() (*credentials.Manager, bool) {
	store, persistent := credentials.DetectStore()
	if !persistent {
		slog.Warn("OS secure store unavailable; credential overrides will not persist " +
			"(set provider credentials via environment variables instead)")
	}

	manager, err := credentials.NewManager(store)
	if err != nil {
		slog.Warn("credential settings unavailable", "error", err)

		return nil, false
	}

	overlayErr := manager.Overlay()
	if overlayErr != nil {
		slog.Warn("failed to apply stored credentials to the environment", "error", overlayErr)
	}

	return manager, persistent
}

// settingsService adapts a credentials.Manager to the operator API's SettingsService, mapping the
// domain status/update types onto the wire types and translating validation errors to client errors.
type settingsService struct {
	manager *credentials.Manager
	// secureStorageAvailable is false when credentials fall back to an in-memory store (no OS secure
	// store). It is surfaced to the SPA so it does not claim secrets are securely persisted.
	secureStorageAvailable bool
}

func (s settingsService) Get(_ context.Context) (api.SettingsResponse, error) {
	statuses, err := s.manager.Status()
	if err != nil {
		return api.SettingsResponse{}, fmt.Errorf("read credential settings: %w", err)
	}

	return api.SettingsResponse{
		Credentials:            toCredentialSettings(statuses),
		SecureStorageAvailable: s.secureStorageAvailable,
	}, nil
}

func (s settingsService) Update(
	ctx context.Context,
	request api.SettingsUpdateRequest,
) (api.SettingsResponse, error) {
	updates := make([]credentials.CredentialUpdate, 0, len(request.Updates))
	for _, update := range request.Updates {
		updates = append(updates, credentials.CredentialUpdate{
			Key:    credentials.Key(update.Key),
			EnvVar: update.EnvVar,
			Value:  update.Value,
		})
	}

	err := s.manager.Update(updates)
	if errors.Is(err, credentials.ErrInvalidEnvVarName) ||
		errors.Is(err, credentials.ErrUnknownCredential) {
		return api.SettingsResponse{}, fmt.Errorf("%w: %w", api.ErrInvalid, err)
	}

	if err != nil {
		return api.SettingsResponse{}, fmt.Errorf("update credential settings: %w", err)
	}

	return s.Get(ctx)
}

func toCredentialSettings(statuses []credentials.CredentialStatus) []api.CredentialSetting {
	settings := make([]api.CredentialSetting, 0, len(statuses))
	for _, status := range statuses {
		settings = append(settings, api.CredentialSetting{
			Key:      string(status.Key),
			Provider: string(credentials.ProviderFor(status.Key)),
			Label:    credentials.Label(status.Key),
			EnvVar:   status.EnvVar,
			Secret:   status.Secret,
			Stored:   status.Stored,
			Source:   status.Source,
			Value:    status.Value,
		})
	}

	return settings
}
