package registryresolver_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/registryresolver"
	"github.com/stretchr/testify/require"
)

// makeDockerConfig builds a Docker config JSON with a single auth entry.
func makeDockerConfig(t *testing.T, host, username, password string) []byte {
	t.Helper()

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	cfg := map[string]any{
		"auths": map[string]any{
			host: map[string]string{"auth": auth},
		},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	return data
}

type credentialTestCase struct {
	name         string
	configData   []byte
	host         string
	wantUser     string
	wantPassword string
}

func credentialTestCases(t *testing.T) []credentialTestCase {
	t.Helper()

	return []credentialTestCase{
		{
			name:         "exact host match",
			configData:   makeDockerConfig(t, "ghcr.io", "user", "pass"),
			host:         "ghcr.io",
			wantUser:     "user",
			wantPassword: "pass",
		},
		{
			name:         "https prefix fallback",
			configData:   makeDockerConfig(t, "https://ghcr.io", "user", "pass"),
			host:         "ghcr.io",
			wantUser:     "user",
			wantPassword: "pass",
		},
		{
			name: "docker.io canonical key fallback",
			configData: makeDockerConfig(
				t,
				"https://index.docker.io/v1/",
				"hub-user",
				"hub-pass",
			),
			host:         "docker.io",
			wantUser:     "hub-user",
			wantPassword: "hub-pass",
		},
		{
			name:         "docker.io exact match preferred over canonical",
			configData:   makeDockerConfig(t, "docker.io", "exact-user", "exact-pass"),
			host:         "docker.io",
			wantUser:     "exact-user",
			wantPassword: "exact-pass",
		},
		{
			name:         "no match returns empty",
			configData:   makeDockerConfig(t, "other.io", "user", "pass"),
			host:         "ghcr.io",
			wantUser:     "",
			wantPassword: "",
		},
		{
			name:         "invalid json returns empty",
			configData:   []byte("not-json"),
			host:         "ghcr.io",
			wantUser:     "",
			wantPassword: "",
		},
		{
			name:         "empty config returns empty",
			configData:   []byte(`{"auths":{}}`),
			host:         "docker.io",
			wantUser:     "",
			wantPassword: "",
		},
	}
}

func TestParseDockerConfigCredentials(t *testing.T) {
	t.Parallel()

	for _, testCase := range credentialTestCases(t) {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotUser, gotPassword := registryresolver.ParseDockerConfigCredentials(
				testCase.configData,
				testCase.host,
			)

			require.Equal(t, testCase.wantUser, gotUser, "username mismatch")
			require.Equal(t, testCase.wantPassword, gotPassword, "password mismatch")
		})
	}
}
