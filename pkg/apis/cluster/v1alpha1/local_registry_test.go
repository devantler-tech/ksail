package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestLocalRegistry_Parse_EmptySpec(t *testing.T) {
	t.Parallel()

	localReg := v1alpha1.LocalRegistry{Registry: ""}
	parsed := localReg.Parse()

	assert.Equal(t, "localhost", parsed.Host)
	assert.Equal(t, v1alpha1.DefaultLocalRegistryPort, parsed.Port)
	assert.Empty(t, parsed.Path)
	assert.Empty(t, parsed.Tag)
	assert.Empty(t, parsed.Username)
	assert.Empty(t, parsed.Password)
}

func TestLocalRegistry_Parse_WhitespaceSpec(t *testing.T) {
	t.Parallel()

	localReg := v1alpha1.LocalRegistry{Registry: "   "}
	parsed := localReg.Parse()

	assert.Equal(t, "localhost", parsed.Host)
	assert.Equal(t, v1alpha1.DefaultLocalRegistryPort, parsed.Port)
}

func TestLocalRegistry_Parse_LocalhostWithPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registry     string
		expectedHost string
		expectedPort int32
	}{
		{
			name:         "localhost_explicit_port",
			registry:     "localhost:5050",
			expectedHost: "localhost",
			expectedPort: 5050,
		},
		{
			name:         "localhost_custom_port",
			registry:     "localhost:9090",
			expectedHost: "localhost",
			expectedPort: 9090,
		},
		{
			name:         "localhost_no_port_defaults",
			registry:     "localhost",
			expectedHost: "localhost",
			expectedPort: v1alpha1.DefaultLocalRegistryPort,
		},
		{
			name:         "127_0_0_1_no_port_defaults",
			registry:     "127.0.0.1",
			expectedHost: "127.0.0.1",
			expectedPort: v1alpha1.DefaultLocalRegistryPort,
		},
		{
			name:         "127_0_0_1_with_port",
			registry:     "127.0.0.1:8080",
			expectedHost: "127.0.0.1",
			expectedPort: 8080,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			localReg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			parsed := localReg.Parse()

			assert.Equal(t, testCase.expectedHost, parsed.Host)
			assert.Equal(t, testCase.expectedPort, parsed.Port)
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestLocalRegistry_Parse_ExternalHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registry     string
		expectedHost string
		expectedPort int32
		expectedPath string
		expectedTag  string
	}{
		{
			name:         "ghcr_with_path",
			registry:     "ghcr.io/myorg/myrepo",
			expectedHost: "ghcr.io",
			expectedPort: 0,
			expectedPath: "myorg/myrepo",
		},
		{
			name:         "ghcr_with_port_and_path",
			registry:     "ghcr.io:443/myorg",
			expectedHost: "ghcr.io",
			expectedPort: 443,
			expectedPath: "myorg",
		},
		{
			name:         "external_host_no_path",
			registry:     "registry.example.com",
			expectedHost: "registry.example.com",
			expectedPort: 0,
		},
		{
			name:         "path_with_tag",
			registry:     "ghcr.io/org/repo:mytag",
			expectedHost: "ghcr.io",
			expectedPort: 0,
			expectedPath: "org/repo",
			expectedTag:  "mytag",
		},
		{
			name:         "path_with_tag_and_port",
			registry:     "registry.io:5000/lib/app:v1",
			expectedHost: "registry.io",
			expectedPort: 5000,
			expectedPath: "lib/app",
			expectedTag:  "v1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			localReg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			parsed := localReg.Parse()

			assert.Equal(t, testCase.expectedHost, parsed.Host)
			assert.Equal(t, testCase.expectedPort, parsed.Port)
			assert.Equal(t, testCase.expectedPath, parsed.Path)
			assert.Equal(t, testCase.expectedTag, parsed.Tag)
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestLocalRegistry_Parse_Credentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		registry         string
		expectedUsername string
		expectedPassword string
		expectedHost     string
		expectedPort     int32
		expectedPath     string
	}{
		{
			name:             "user_and_password",
			registry:         "admin:secret@registry.io:5000/path",
			expectedUsername: "admin",
			expectedPassword: "secret",
			expectedHost:     "registry.io",
			expectedPort:     5000,
			expectedPath:     "path",
		},
		{
			name:             "user_only",
			registry:         "admin@registry.io",
			expectedUsername: "admin",
			expectedPassword: "",
			expectedHost:     "registry.io",
			expectedPort:     0,
		},
		{
			name:             "env_var_placeholders_not_expanded_by_parse",
			registry:         "${USER}:${PASS}@ghcr.io:443/org",
			expectedUsername: "${USER}",
			expectedPassword: "${PASS}",
			expectedHost:     "ghcr.io",
			expectedPort:     443,
			expectedPath:     "org",
		},
		{
			name:             "no_credentials",
			registry:         "ghcr.io/org",
			expectedUsername: "",
			expectedPassword: "",
			expectedHost:     "ghcr.io",
			expectedPort:     0,
			expectedPath:     "org",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			localReg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			parsed := localReg.Parse()

			assert.Equal(t, testCase.expectedUsername, parsed.Username)
			assert.Equal(t, testCase.expectedPassword, parsed.Password)
			assert.Equal(t, testCase.expectedHost, parsed.Host)
			assert.Equal(t, testCase.expectedPort, parsed.Port)
			assert.Equal(t, testCase.expectedPath, parsed.Path)
		})
	}
}

func TestLocalRegistry_ResolvedHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		expected string
	}{
		{"empty_defaults_to_localhost", "", "localhost"},
		{"ghcr", "ghcr.io/org", "ghcr.io"},
		{"localhost_with_port", "localhost:5050", "localhost"},
		{"custom_host", "myhost.io:9090/path", "myhost.io"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			localReg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.expected, localReg.ResolvedHost())
		})
	}
}

func TestLocalRegistry_ResolvedPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		expected int32
	}{
		{"empty_defaults", "", v1alpha1.DefaultLocalRegistryPort},
		{"localhost_no_port", "localhost", v1alpha1.DefaultLocalRegistryPort},
		{"explicit_port", "localhost:9090", 9090},
		{"external_no_port", "ghcr.io/org", 0},
		{"external_explicit_port", "ghcr.io:443/org", 443},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			localReg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.expected, localReg.ResolvedPort())
		})
	}
}

func TestLocalRegistry_ResolvedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		expected string
	}{
		{"empty_registry", "", ""},
		{"no_path", "localhost:5050", ""},
		{"with_path", "ghcr.io/org/repo", "org/repo"},
		{"with_path_and_tag_strips_tag", "ghcr.io/org/repo:v1", "org/repo"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			localReg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.expected, localReg.ResolvedPath())
		})
	}
}

func TestRedactRegistryCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		expected string
	}{
		{"empty", "", ""},
		{"no_credentials_with_port", "localhost:5050", "localhost:5050"},
		{"no_credentials_with_path", "ghcr.io/org/repo", "ghcr.io/org/repo"},
		{"username_only_no_password", "user@ghcr.io", "user@ghcr.io"},
		{"empty_password", "user:@ghcr.io", "user:@ghcr.io"},
		{"empty_username_masks_password", ":ghp_secret@ghcr.io", ":****@ghcr.io"},
		{"masks_pat", "user:ghp_secret@ghcr.io/org", "user:****@ghcr.io/org"},
		{
			"masks_pat_keeps_port_and_path",
			"user:ghp_secret@ghcr.io:443/org/repo:tag",
			"user:****@ghcr.io:443/org/repo:tag",
		},
		{
			"masks_resolved_github_actor_credentials",
			"GITHUB_ACTOR:ghp_abc123@ghcr.io/devantler-tech",
			"GITHUB_ACTOR:****@ghcr.io/devantler-tech",
		},
		{"masks_password_containing_colons", "user:p:a:ss@ghcr.io", "user:****@ghcr.io"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(
				t,
				testCase.expected,
				v1alpha1.RedactRegistryCredentials(testCase.registry),
			)
		})
	}
}

// ---------------------------------------------------------------------------
// parseHostAndPort edge cases
// ---------------------------------------------------------------------------

func TestLocalRegistry_Parse_ColonAtStart(t *testing.T) {
	t.Parallel()

	reg := v1alpha1.LocalRegistry{Registry: ":5000"}
	parsed := reg.Parse()

	// colonIdx == 0 which is <= 0, so the whole string is treated as host
	assert.Equal(t, ":5000", parsed.Host)
}

func TestLocalRegistry_Parse_InvalidPortTreatedAsHost(t *testing.T) {
	t.Parallel()

	reg := v1alpha1.LocalRegistry{Registry: "myhost:notaport"}
	parsed := reg.Parse()

	assert.Equal(t, "myhost:notaport", parsed.Host)
	assert.Equal(t, int32(0), parsed.Port)
}
