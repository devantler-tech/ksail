package registryresolver_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/registryresolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testHost     = "registry.example.com"
	testUsername = "testuser"
	testPassword = "testpass"
)

// TestParseDockerConfigCredentials_ExactHostMatch tests credential parsing with exact host match.
func TestParseDockerConfigCredentials_ExactHostMatch(t *testing.T) {
	t.Parallel()

	host := testHost
	username := testUsername
	password := testPassword

	// Create Docker config JSON with exact host match
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	config := map[string]any{
		"auths": map[string]any{
			host: map[string]any{
				"auth": auth,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Equal(t, username, gotUsername)
	assert.Equal(t, password, gotPassword)
}

// TestParseDockerConfigCredentials_HTTPSPrefixMatch tests credential parsing with https:// prefix.
func TestParseDockerConfigCredentials_HTTPSPrefixMatch(t *testing.T) {
	t.Parallel()

	host := testHost
	username := testUsername
	password := testPassword

	// Create Docker config JSON with https:// prefix
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	config := map[string]any{
		"auths": map[string]any{
			"https://" + host: map[string]any{
				"auth": auth,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - should find it with https:// prefix even though we search for bare host
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Equal(t, username, gotUsername)
	assert.Equal(t, password, gotPassword)
}

// TestParseDockerConfigCredentials_NoMatch tests credential parsing when host doesn't match.
func TestParseDockerConfigCredentials_NoMatch(t *testing.T) {
	t.Parallel()

	host := testHost
	username := "testuser"
	password := "testpass"

	// Create Docker config JSON with different host
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	config := map[string]any{
		"auths": map[string]any{
			"different.registry.com": map[string]any{
				"auth": auth,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - should return empty strings
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Empty(t, gotUsername)
	assert.Empty(t, gotPassword)
}

// TestParseDockerConfigCredentials_InvalidJSON tests credential parsing with invalid JSON.
func TestParseDockerConfigCredentials_InvalidJSON(t *testing.T) {
	t.Parallel()

	host := testHost
	configData := []byte("invalid json {{{")

	// Parse credentials - should handle error gracefully
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Empty(t, gotUsername)
	assert.Empty(t, gotPassword)
}

// TestParseDockerConfigCredentials_InvalidBase64 tests credential parsing with invalid base64.
func TestParseDockerConfigCredentials_InvalidBase64(t *testing.T) {
	t.Parallel()

	host := testHost

	// Create Docker config JSON with invalid base64 auth
	config := map[string]any{
		"auths": map[string]any{
			host: map[string]any{
				"auth": "not-valid-base64!@#$",
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - should handle error gracefully
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Empty(t, gotUsername)
	assert.Empty(t, gotPassword)
}

// TestParseDockerConfigCredentials_MalformedCredentials tests parsing with malformed credentials.
func TestParseDockerConfigCredentials_MalformedCredentials(t *testing.T) {
	t.Parallel()

	host := testHost

	tests := []struct {
		name  string
		creds string
	}{
		{
			name:  "missing colon separator",
			creds: "usernameonly",
		},
		{
			name:  "empty string",
			creds: "",
		},
		{
			name:  "only colon",
			creds: ":",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Create Docker config JSON with malformed credentials
			auth := base64.StdEncoding.EncodeToString([]byte(testCase.creds))
			config := map[string]any{
				"auths": map[string]any{
					host: map[string]any{
						"auth": auth,
					},
				},
			}

			configData, err := json.Marshal(config)
			require.NoError(t, err)

			// Parse credentials - should return empty strings for malformed input
			gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(
				configData,
				host,
			)

			assert.Empty(t, gotUsername)
			assert.Empty(t, gotPassword)
		})
	}
}

// TestParseDockerConfigCredentials_EmptyAuth tests credential parsing with empty auth field.
func TestParseDockerConfigCredentials_EmptyAuth(t *testing.T) {
	t.Parallel()

	host := testHost

	// Create Docker config JSON with empty auth
	config := map[string]any{
		"auths": map[string]any{
			host: map[string]any{
				"auth": "",
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - should handle empty auth gracefully
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Empty(t, gotUsername)
	assert.Empty(t, gotPassword)
}

// TestParseDockerConfigCredentials_PasswordWithColon tests passwords containing colons.
func TestParseDockerConfigCredentials_PasswordWithColon(t *testing.T) {
	t.Parallel()

	host := testHost
	username := "testuser"
	password := "test:pass:with:colons"

	// Create Docker config JSON
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	config := map[string]any{
		"auths": map[string]any{
			host: map[string]any{
				"auth": auth,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - should handle password with colons correctly
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Equal(t, username, gotUsername)
	assert.Equal(t, password, gotPassword)
}

// TestParseDockerConfigCredentials_MultipleHosts tests config with multiple hosts.
func TestParseDockerConfigCredentials_MultipleHosts(t *testing.T) {
	t.Parallel()

	host1 := "registry1.example.com"
	host2 := "registry2.example.com"
	username1 := "user1"
	password1 := "pass1"
	username2 := "user2"
	password2 := "pass2"

	// Create Docker config JSON with multiple hosts
	auth1 := base64.StdEncoding.EncodeToString([]byte(username1 + ":" + password1))
	auth2 := base64.StdEncoding.EncodeToString([]byte(username2 + ":" + password2))
	config := map[string]any{
		"auths": map[string]any{
			host1: map[string]any{
				"auth": auth1,
			},
			host2: map[string]any{
				"auth": auth2,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials for host1
	gotUsername1, gotPassword1 := registryresolver.ParseDockerConfigCredentials(configData, host1)
	assert.Equal(t, username1, gotUsername1)
	assert.Equal(t, password1, gotPassword1)

	// Parse credentials for host2
	gotUsername2, gotPassword2 := registryresolver.ParseDockerConfigCredentials(configData, host2)
	assert.Equal(t, username2, gotUsername2)
	assert.Equal(t, password2, gotPassword2)
}

// TestParseDockerConfigCredentials_EmptyConfig tests parsing with empty config.
func TestParseDockerConfigCredentials_EmptyConfig(t *testing.T) {
	t.Parallel()

	host := testHost

	// Create empty Docker config JSON
	config := map[string]any{
		"auths": map[string]any{},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - should return empty strings
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Empty(t, gotUsername)
	assert.Empty(t, gotPassword)
}

// TestParseDockerConfigCredentials_MissingAuthsField tests parsing without auths field.
func TestParseDockerConfigCredentials_MissingAuthsField(t *testing.T) {
	t.Parallel()

	host := testHost

	// Create Docker config JSON without auths field
	config := map[string]any{
		"other": "data",
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - should handle missing auths gracefully
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Empty(t, gotUsername)
	assert.Empty(t, gotPassword)
}

// TestParseDockerConfigCredentials_DockerHubFormat tests Docker Hub registry format.
func TestParseDockerConfigCredentials_DockerHubFormat(t *testing.T) {
	t.Parallel()

	host := "docker.io"
	username := "dockeruser"
	password := "dockerpass"

	// Create Docker config JSON with docker.io
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	config := map[string]any{
		"auths": map[string]any{
			"https://index.docker.io/v1/": map[string]any{
				"auth": auth,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials - looking for "docker.io" won't match the https URL
	// This documents current behavior
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	// Current implementation won't match this - it only tries exact match and https://host
	// not full paths like https://index.docker.io/v1/
	assert.Empty(t, gotUsername)
	assert.Empty(t, gotPassword)
}

// TestParseDockerConfigCredentials_GHCRFormat tests GitHub Container Registry format.
func TestParseDockerConfigCredentials_GHCRFormat(t *testing.T) {
	t.Parallel()

	host := "ghcr.io"
	username := "ghcruser"
	password := "ghcrtoken"

	// Create Docker config JSON with ghcr.io
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	config := map[string]any{
		"auths": map[string]any{
			host: map[string]any{
				"auth": auth,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Parse credentials
	gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(configData, host)

	assert.Equal(t, username, gotUsername)
	assert.Equal(t, password, gotPassword)
}

// TestParseDockerConfigCredentials_LocalhostFormat tests localhost registry format.
func TestParseDockerConfigCredentials_LocalhostFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		host      string
		configKey string
	}{
		{
			name:      "localhost with port",
			host:      "localhost:5000",
			configKey: "localhost:5000",
		},
		{
			name:      "localhost bare",
			host:      "localhost",
			configKey: "localhost",
		},
		{
			name:      "IPv4 with port",
			host:      "127.0.0.1:5000",
			configKey: "127.0.0.1:5000",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			username := "localuser"
			password := "localpass"

			// Create Docker config JSON
			auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
			config := map[string]any{
				"auths": map[string]any{
					testCase.configKey: map[string]any{
						"auth": auth,
					},
				},
			}

			configData, err := json.Marshal(config)
			require.NoError(t, err)

			// Parse credentials
			gotUsername, gotPassword := registryresolver.ParseDockerConfigCredentials(
				configData,
				testCase.host,
			)

			assert.Equal(t, username, gotUsername)
			assert.Equal(t, password, gotPassword)
		})
	}
}
