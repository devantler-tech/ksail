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
	response      api.SettingsResponse
	lastUpdate    *api.SettingsUpdateRequest
	updateErr     error
	app           api.AppSettings
	lastAppUpdate *api.AppSettings
	appUpdateErr  error
	testResult    api.CredentialTestResult
	testErr       error
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

func (f *fakeSettings) AppSettings(context.Context) (api.AppSettings, error) {
	return f.app, nil
}

func (f *fakeSettings) UpdateAppSettings(
	_ context.Context,
	request api.AppSettings,
) (api.AppSettings, error) {
	f.lastAppUpdate = &request
	if f.appUpdateErr != nil {
		return api.AppSettings{}, f.appUpdateErr
	}

	return f.app, nil
}

func (f *fakeSettings) TestConnection(
	_ context.Context,
	provider string,
) (api.CredentialTestResult, error) {
	if f.testErr != nil {
		return api.CredentialTestResult{}, f.testErr
	}

	result := f.testResult
	result.Provider = provider

	return result, nil
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

func TestGetAppSettingsReturnsPreferences(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{app: api.AppSettings{
		Editor: "code --wait",
		Chat:   api.ChatSettings{Model: "gpt-5", ReasoningEffort: "high"},
	}}
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), Settings: settings}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/settings/app", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"editor":"code --wait"`)
	assert.Contains(t, recorder.Body.String(), `"reasoningEffort":"high"`)
}

func TestUpdateAppSettingsDecodesAndDelegates(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{}
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), Settings: settings}

	body := `{"editor":"vim","chat":{"model":"gpt-5-mini","reasoningEffort":"low"}}`
	recorder := doRequest(server.Handler(), http.MethodPut, "/api/v1/settings/app", body)

	assert.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, settings.lastAppUpdate)
	assert.Equal(t, "vim", settings.lastAppUpdate.Editor)
	assert.Equal(t, "gpt-5-mini", settings.lastAppUpdate.Chat.Model)
	assert.Equal(t, "low", settings.lastAppUpdate.Chat.ReasoningEffort)
}

func TestUpdateAppSettingsMapsInvalidToUnprocessable(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{appUpdateErr: api.ErrInvalid}
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), Settings: settings}

	recorder := doRequest(
		server.Handler(),
		http.MethodPut,
		"/api/v1/settings/app",
		`{"chat":{"reasoningEffort":"bananas"}}`,
	)

	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
}

func TestTestConnectionReturnsResult(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{testResult: api.CredentialTestResult{OK: true, Message: "ok"}}
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), Settings: settings}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/settings/credentials/hetzner/test",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"provider":"hetzner"`)
	assert.Contains(t, recorder.Body.String(), `"ok":true`)
}

func TestTestConnectionMapsInvalidToUnprocessable(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{testErr: api.ErrInvalid}
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), Settings: settings}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/settings/credentials/nope/test",
		"",
	)

	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
}
