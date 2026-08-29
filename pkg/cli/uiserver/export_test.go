package uiserver

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// UpdateAppSettingsForTest exposes the Settings adapter's app-settings path so the package's
// external test can assert how domain validation errors map onto the API's client-error type,
// without standing up an HTTP server.
func UpdateAppSettingsForTest(
	ctx context.Context,
	manager *credentials.Manager,
	request api.AppSettings,
) (api.AppSettings, error) {
	return settingsService{manager: manager}.UpdateAppSettings(ctx, request)
}
