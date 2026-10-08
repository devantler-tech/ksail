package clusterapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestLocalAuthenticationRenewalUsesSelectedLegacyContext(t *testing.T) {
	t.Parallel()
	fixture := testutil.NewSyntheticSSO(t, "legacy")
	service := &Service{
		restConfigForCluster: func(_ context.Context, name string) (*rest.Config, error) {
			require.Equal(t, "selected", name)

			return &rest.Config{ExecProvider: fixture.Provider}, nil
		},
	}
	server := &api.Server{
		Service: service, Mode: api.ModeLocal, UIOrigin: "http://127.0.0.1:9090",
		AWSSSORenewalEnabled: func() bool { return true },
	}
	call := authenticationCaller(t, server.Handler())
	initial := call(http.MethodGet, "", "", "http://127.0.0.1:9090")
	require.Equal(t, http.StatusOK, initial.Code, initial.Body.String())

	info := decodeAuthentication(t, initial)
	assert.True(t, info.Enabled)
	assert.True(t, info.Required)

	_, err := os.Stat(filepath.Join(fixture.Root, "logins"))
	require.ErrorIs(t, err, os.ErrNotExist, "background status may not start sign-in")
	assert.Equal(
		t,
		http.StatusForbidden,
		call(http.MethodPost, "/renew", "{}", "https://other.example.invalid").Code,
	)
	assert.NotEqual(
		t,
		http.StatusNoContent,
		call(
			http.MethodPost,
			"/renew",
			`{"profile":"other","command":"unsafe"}`,
			"http://127.0.0.1:9090",
		).Code,
	)
	renewed := call(http.MethodPost, "/renew", "{}", "http://127.0.0.1:9090")
	require.Equal(t, http.StatusNoContent, renewed.Code, renewed.Body.String())
	assert.NotContains(t, renewed.Body.String(), "SENSITIVE")

	after := call(http.MethodGet, "", "", "http://127.0.0.1:9090")
	info = decodeAuthentication(t, after)
	assert.False(t, info.Required)
}

func authenticationCaller(
	t *testing.T,
	handler http.Handler,
) func(string, string, string, string) *httptest.ResponseRecorder {
	t.Helper()

	return func(method, suffix, body, origin string) *httptest.ResponseRecorder {
		t.Helper()

		url := "http://127.0.0.1:9090/api/v1/clusters/default/selected/authentication" + suffix
		req := httptest.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)

		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)

		return result
	}
}

func TestLocalAuthenticationDefaultOffAndReadOnlyNeverLogin(t *testing.T) {
	t.Parallel()

	for _, readOnly := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "default off", true: "read only"}[readOnly],
			func(t *testing.T) {
				t.Parallel()
				fixture := testutil.NewSyntheticSSO(t, "legacy")
				service := &Service{
					restConfigForCluster: func(context.Context, string) (*rest.Config, error) {
						t.Fatal("disabled sign-in must not even resolve a credential target")

						return nil, api.ErrNotSupported
					},
				}
				server := &api.Server{
					Service: service, Mode: api.ModeLocal, ReadOnly: readOnly,
					AWSSSORenewalEnabled: func() bool { return readOnly },
				}
				req := httptest.NewRequestWithContext(
					t.Context(),
					http.MethodPost,
					"http://localhost/api/v1/clusters/default/selected/authentication/renew",
					strings.NewReader("{}"),
				)
				req.Header.Set("Content-Type", "application/json")

				result := httptest.NewRecorder()
				server.Handler().ServeHTTP(result, req)
				assert.Equal(t, http.StatusForbidden, result.Code)

				_, err := os.Stat(filepath.Join(fixture.Root, "logins"))
				assert.ErrorIs(t, err, os.ErrNotExist)
			},
		)
	}
}

func decodeAuthentication(
	t *testing.T,
	response *httptest.ResponseRecorder,
) api.ClusterAuthentication {
	t.Helper()

	var info api.ClusterAuthentication
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &info))

	return info
}
