package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

func TestPolicyEngineSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantValue v1alpha1.PolicyEngine
		wantError bool
	}{
		{
			name:      "sets_kyverno",
			input:     "Kyverno",
			wantValue: v1alpha1.PolicyEngineKyverno,
			wantError: false,
		},
		{
			name:      "sets_gatekeeper",
			input:     "Gatekeeper",
			wantValue: v1alpha1.PolicyEngineGatekeeper,
			wantError: false,
		},
		{
			name:      "sets_none",
			input:     "None",
			wantValue: v1alpha1.PolicyEngineNone,
			wantError: false,
		},
		{
			name:      "case_insensitive_kyverno",
			input:     "kyverno",
			wantValue: v1alpha1.PolicyEngineKyverno,
			wantError: false,
		},
		{
			name:      "invalid_value",
			input:     "invalid",
			wantValue: "",
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var policyEngine v1alpha1.PolicyEngine

			err := policyEngine.Set(testCase.input)

			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantValue, policyEngine)
			}
		})
	}
}

func TestDistribution_ProvidesMetricsServerByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         bool
		description  string
	}{
		{
			name:         "returns_true_for_k3s",
			distribution: v1alpha1.DistributionK3s,
			want:         true,
			description:  "K3s should provide metrics-server by default",
		},
		{
			name:         "returns_false_for_kind",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
			description:  "Kind should not provide metrics-server by default",
		},
		{
			name:         "returns_false_for_unknown_distribution",
			distribution: v1alpha1.Distribution("unknown"),
			want:         false,
			description:  "Unknown distributions should not provide metrics-server by default",
		},
		{
			name:         "returns_false_for_empty_distribution",
			distribution: v1alpha1.Distribution(""),
			want:         false,
			description:  "Empty distribution should not provide metrics-server by default",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesMetricsServerByDefault()

			assert.Equal(t, testCase.want, result, testCase.description)
		})
	}
}

func TestDistribution_ProvidesStorageByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         bool
		description  string
	}{
		{
			name:         "returns_true_for_k3s",
			distribution: v1alpha1.DistributionK3s,
			want:         true,
			description:  "K3s should provide storage by default",
		},
		{
			name:         "returns_false_for_kind",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
			description:  "Kind should not provide storage by default",
		},
		{
			name:         "returns_false_for_unknown_distribution",
			distribution: v1alpha1.Distribution("unknown"),
			want:         false,
			description:  "Unknown distributions should not provide storage by default",
		},
		{
			name:         "returns_false_for_empty_distribution",
			distribution: v1alpha1.Distribution(""),
			want:         false,
			description:  "Empty distribution should not provide storage by default",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesStorageByDefault()

			assert.Equal(t, testCase.want, result, testCase.description)
		})
	}
}

func TestLocalRegistry_Parse_ExtractsTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		wantHost string
		wantPath string
		wantTag  string
	}{
		{
			name:     "external_registry_with_tag",
			registry: "ghcr.io/org/repo:v1.0.0",
			wantHost: "ghcr.io",
			wantPath: "org/repo",
			wantTag:  "v1.0.0",
		},
		{
			name:     "external_registry_with_complex_tag",
			registry: "ghcr.io/devantler-tech/ksail/manifests:k3s-docker-abc1234",
			wantHost: "ghcr.io",
			wantPath: "devantler-tech/ksail/manifests",
			wantTag:  "k3s-docker-abc1234",
		},
		{
			name:     "external_registry_without_tag",
			registry: "ghcr.io/org/repo",
			wantHost: "ghcr.io",
			wantPath: "org/repo",
			wantTag:  "",
		},
		{
			name:     "external_registry_with_credentials_and_tag",
			registry: "user:pass@ghcr.io/org/repo:dev",
			wantHost: "ghcr.io",
			wantPath: "org/repo",
			wantTag:  "dev",
		},
		{
			name:     "local_registry_no_tag",
			registry: "localhost:5000",
			wantHost: "localhost",
			wantPath: "",
			wantTag:  "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			parsed := reg.Parse()

			assert.Equal(t, testCase.wantHost, parsed.Host)
			assert.Equal(t, testCase.wantPath, parsed.Path)
			assert.Equal(t, testCase.wantTag, parsed.Tag)
		})
	}
}

func TestLocalRegistry_ResolvedTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		wantTag  string
	}{
		{
			name:     "returns_tag_when_present",
			registry: "ghcr.io/org/repo:mytag",
			wantTag:  "mytag",
		},
		{
			name:     "returns_empty_when_no_tag",
			registry: "ghcr.io/org/repo",
			wantTag:  "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.wantTag, reg.ResolvedTag())
		})
	}
}

func TestLocalRegistry_Enabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		want     bool
	}{
		{
			name:     "returns_true_for_non_empty",
			registry: "localhost:5000",
			want:     true,
		},
		{
			name:     "returns_true_for_external_registry",
			registry: "ghcr.io/org/repo",
			want:     true,
		},
		{
			name:     "returns_false_for_empty",
			registry: "",
			want:     false,
		},
		{
			name:     "returns_false_for_whitespace_only",
			registry: "   ",
			want:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.want, reg.Enabled())
		})
	}
}

func TestLocalRegistry_IsExternal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		want     bool
	}{
		{
			name:     "localhost_is_not_external",
			registry: "localhost:5000",
			want:     false,
		},
		{
			name:     "127_0_0_1_is_not_external",
			registry: "127.0.0.1:5000",
			want:     false,
		},
		{
			name:     "ghcr_io_is_external",
			registry: "ghcr.io/org/repo",
			want:     true,
		},
		{
			name:     "docker_io_is_external",
			registry: "docker.io/library/nginx",
			want:     true,
		},
		{
			name:     "empty_defaults_to_localhost_not_external",
			registry: "",
			want:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.want, reg.IsExternal())
		})
	}
}

func TestLocalRegistry_HasCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		want     bool
	}{
		{
			name:     "no_credentials",
			registry: "ghcr.io/org/repo",
			want:     false,
		},
		{
			name:     "username_and_password",
			registry: "user:pass@ghcr.io/org/repo",
			want:     true,
		},
		{
			name:     "username_only",
			registry: "user@ghcr.io/org/repo",
			want:     true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.want, reg.HasCredentials())
		})
	}
}

func TestLocalRegistry_ResolvedHostPortPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		wantHost string
		wantPort int32
		wantPath string
	}{
		{
			name:     "localhost_with_port",
			registry: "localhost:5000",
			wantHost: "localhost",
			wantPort: 5000,
			wantPath: "",
		},
		{
			name:     "localhost_without_port_uses_default",
			registry: "localhost",
			wantHost: "localhost",
			wantPort: v1alpha1.DefaultLocalRegistryPort,
			wantPath: "",
		},
		{
			name:     "external_registry_with_path",
			registry: "ghcr.io/org/repo",
			wantHost: "ghcr.io",
			wantPort: 0, // No port for external registries
			wantPath: "org/repo",
		},
		{
			name:     "empty_registry_defaults",
			registry: "",
			wantHost: "localhost",
			wantPort: v1alpha1.DefaultLocalRegistryPort,
			wantPath: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			assert.Equal(t, testCase.wantHost, reg.ResolvedHost())
			assert.Equal(t, testCase.wantPort, reg.ResolvedPort())
			assert.Equal(t, testCase.wantPath, reg.ResolvedPath())
		})
	}
}

type resolveCredentialsTestCase struct {
	name         string
	registry     string
	envVars      map[string]string
	wantUsername string
	wantPassword string
}

func getResolveCredentialsTestCases() []resolveCredentialsTestCase {
	return []resolveCredentialsTestCase{
		{
			name:         "literal_credentials",
			registry:     "myuser:mypass@ghcr.io/org/repo",
			wantUsername: "myuser",
			wantPassword: "mypass",
		},
		{
			name:         "env_var_credentials",
			registry:     "${REGISTRY_USER}:${REGISTRY_PASS}@ghcr.io/org/repo",
			envVars:      map[string]string{"REGISTRY_USER": "envuser", "REGISTRY_PASS": "envpass"},
			wantUsername: "envuser",
			wantPassword: "envpass",
		},
		{
			name:         "mixed_literal_and_env_var",
			registry:     "literaluser:${REGISTRY_PASS}@ghcr.io/org/repo",
			envVars:      map[string]string{"REGISTRY_PASS": "secret123"},
			wantUsername: "literaluser",
			wantPassword: "secret123",
		},
		{
			name:         "undefined_env_var_becomes_empty",
			registry:     "${UNDEFINED_USER}:${UNDEFINED_PASS}@ghcr.io/org/repo",
			wantUsername: "",
			wantPassword: "",
		},
		{
			name:         "no_credentials",
			registry:     "ghcr.io/org/repo",
			wantUsername: "",
			wantPassword: "",
		},
		{
			name:         "username_only",
			registry:     "onlyuser@ghcr.io/org/repo",
			wantUsername: "onlyuser",
			wantPassword: "",
		},
		{
			name:         "empty_registry",
			registry:     "",
			wantUsername: "",
			wantPassword: "",
		},
	}
}

func TestLocalRegistry_ResolveCredentials(t *testing.T) {
	// Note: Cannot use t.Parallel() when using t.Setenv()
	tests := getResolveCredentialsTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			for key, value := range testCase.envVars {
				t.Setenv(key, value)
			}

			reg := v1alpha1.LocalRegistry{Registry: testCase.registry}
			gotUsername, gotPassword := reg.ResolveCredentials()

			assert.Equal(t, testCase.wantUsername, gotUsername)
			assert.Equal(t, testCase.wantPassword, gotPassword)
		})
	}
}
