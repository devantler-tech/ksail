package uiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
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

func (s settingsService) AppSettings(_ context.Context) (api.AppSettings, error) {
	return toAPIAppSettings(s.manager.AppSettings()), nil
}

func (s settingsService) UpdateAppSettings(
	ctx context.Context,
	request api.AppSettings,
) (api.AppSettings, error) {
	err := s.manager.UpdateAppSettings(credentials.AppSettings{
		Editor:              request.Editor,
		ChatModel:           request.Chat.Model,
		ChatReasoningEffort: request.Chat.ReasoningEffort,
	})
	if errors.Is(err, credentials.ErrInvalidReasoningEffort) {
		return api.AppSettings{}, fmt.Errorf("%w: %w", api.ErrInvalid, err)
	}

	if err != nil {
		return api.AppSettings{}, fmt.Errorf("update app settings: %w", err)
	}

	return s.AppSettings(ctx)
}

// TestConnection validates the stored credentials for a provider by making one cheap authenticated
// API call. Credentials resolve from the environment (the Manager overlays stored secrets there), so
// the zero options use the default variable names. A failed connection is reported via the result;
// an unsupported provider returns an ErrInvalid error so the handler answers 400.
func (s settingsService) TestConnection(
	ctx context.Context,
	provider string,
) (api.CredentialTestResult, error) {
	var err error

	switch strings.ToLower(provider) {
	case "hetzner":
		err = hetzner.ValidateCredentials(ctx, v1alpha1.OptionsHetzner{})
	case "omni":
		err = omni.ValidateCredentials(ctx, v1alpha1.OptionsOmni{})
	default:
		return api.CredentialTestResult{}, fmt.Errorf(
			"%w: connection testing is not supported for provider %q",
			api.ErrInvalid,
			provider,
		)
	}

	result := api.CredentialTestResult{
		Provider: provider,
		OK:       err == nil,
		Message:  "Connection successful.",
	}
	if err != nil {
		result.Message = err.Error()
	}

	return result, nil
}

func toAPIAppSettings(app credentials.AppSettings) api.AppSettings {
	return api.AppSettings{
		Editor: app.Editor,
		Chat: api.ChatSettings{
			Model:           app.ChatModel,
			ReasoningEffort: app.ChatReasoningEffort,
		},
	}
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
