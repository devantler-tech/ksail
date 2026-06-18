package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSettings is a stub SettingsService capturing the last update for assertions.
type fakeSettings struct {
	response   api.SettingsResponse
	lastUpdate *api.SettingsUpdateRequest
	updateErr  error
}

func (f *fakeSettings) Get(context.Context) (api.SettingsResponse, error) {
	return f.response, nil
}

func (f *fakeSettings) Update(
	_ context.Context,
	request api.SettingsUpdateRequest,
) (api.SettingsResponse, error) {
	f.lastUpdate = &request
	if f.updateErr != nil {
		return api.SettingsResponse{}, f.updateErr
	}

	return f.response, nil
}

func TestSettingsRoutesAbsentWhenServiceUnset(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/settings", "")

	// Without a SettingsService the route is not registered; the SPA catch-all is not wired in this
	// server either, so the request falls through to 404.
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestConfigReportsSettingsEnabled(t *testing.T) {
	t.Parallel()

	server := &api.Server{
		Service:  operator.NewCRClusterService(newClient(t)),
		Settings: &fakeSettings{},
	}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"settingsEnabled":true`)
}

func TestGetSettingsReturnsCredentials(t *testing.T) {
	t.Parallel()

	server := &api.Server{
		Service: operator.NewCRClusterService(newClient(t)),
		Settings: &fakeSettings{response: api.SettingsResponse{
			Credentials: []api.CredentialSetting{
				{Key: "hetzner.token", Provider: "Hetzner", EnvVar: "HCLOUD_TOKEN", Secret: true},
			},
			SecureStorageAvailable: true,
		}},
	}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/settings", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"key":"hetzner.token"`)
	assert.Contains(t, recorder.Body.String(), `"secret":true`)
	assert.Contains(t, recorder.Body.String(), `"secureStorageAvailable":true`)
}

func TestUpdateSettingsDecodesAndDelegates(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{}
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), Settings: settings}

	body := `{"updates":[{"key":"hetzner.token","envVar":"MY_HCLOUD","value":"tok"}]}`
	recorder := doRequest(server.Handler(), http.MethodPut, "/api/v1/settings", body)

	assert.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, settings.lastUpdate)
	require.Len(t, settings.lastUpdate.Updates, 1)
	assert.Equal(t, "hetzner.token", settings.lastUpdate.Updates[0].Key)
	require.NotNil(t, settings.lastUpdate.Updates[0].EnvVar)
	assert.Equal(t, "MY_HCLOUD", *settings.lastUpdate.Updates[0].EnvVar)
	require.NotNil(t, settings.lastUpdate.Updates[0].Value)
	assert.Equal(t, "tok", *settings.lastUpdate.Updates[0].Value)
}

func TestUpdateSettingsMapsInvalidToUnprocessable(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{updateErr: api.ErrInvalid}
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), Settings: settings}

	recorder := doRequest(
		server.Handler(),
		http.MethodPut,
		"/api/v1/settings",
		`{"updates":[{"key":"hetzner.token","envVar":"bad name"}]}`,
	)

	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
}
