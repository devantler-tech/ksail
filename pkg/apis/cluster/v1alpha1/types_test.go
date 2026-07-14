package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

// distributionBoolTestCase defines a test case for Distribution methods that return bool.
type distributionBoolTestCase struct {
	name         string
	distribution v1alpha1.Distribution
	want         bool
	description  string
}

// distributionBoolTestCases returns shared test cases for `Provides*ByDefault`
// methods where K3s returns true, and everything else returns false.
func distributionBoolTestCases(featureName string) []distributionBoolTestCase {
	return []distributionBoolTestCase{
		{
			name:         "returns_true_for_k3s",
			distribution: v1alpha1.DistributionK3s,
			want:         true,
			description:  "K3s should provide " + featureName + " by default",
		},
		{
			name:         "returns_false_for_vcluster",
			distribution: v1alpha1.DistributionVCluster,
			want:         false,
			description:  "VCluster (Vind with Distro: k8s) should not provide " + featureName + " by default",
		},
		{
			name:         "returns_false_for_talos",
			distribution: v1alpha1.DistributionTalos,
			want:         false,
			description:  "Talos should not provide " + featureName + " by default",
		},
		{
			name:         "returns_false_for_kind",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
			description:  "Kind should not provide " + featureName + " by default",
		},
		{
			name:         "returns_false_for_unknown_distribution",
			distribution: v1alpha1.Distribution("unknown"),
			want:         false,
			description:  "Unknown distributions should not provide " + featureName + " by default",
		},
		{
			name:         "returns_false_for_empty_distribution",
			distribution: v1alpha1.Distribution(""),
			want:         false,
			description:  "Empty distribution should not provide " + featureName + " by default",
		},
	}
}

func TestDistribution_ProvidesMetricsServerByDefault(t *testing.T) {
	t.Parallel()

	for _, testCase := range distributionBoolTestCases("metrics-server") {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesMetricsServerByDefault()

			assert.Equal(t, testCase.want, result, testCase.description)
		})
	}

	t.Run("returns_false_for_eks", func(t *testing.T) {
		t.Parallel()

		dist := v1alpha1.DistributionEKS
		assert.False(t, dist.ProvidesMetricsServerByDefault())
	})
}

func TestDistribution_ProvidesStorageByDefault(t *testing.T) {
	t.Parallel()

	for _, testCase := range distributionBoolTestCases("storage") {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesStorageByDefault()

			assert.Equal(t, testCase.want, result, testCase.description)
		})
	}

	t.Run("returns_true_for_eks", func(t *testing.T) {
		t.Parallel()

		dist := v1alpha1.DistributionEKS
		assert.True(
			t, dist.ProvidesStorageByDefault(),
			"EKS scaffolds the EBS CSI addon by default",
		)
	})
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
		{
			name:     "password_only",
			registry: ":pass@ghcr.io/org/repo",
			want:     false,
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

func TestLocalRegistry_HasCredentialsRejectsResolvedPasswordOnlyAuth(t *testing.T) {
	t.Setenv("GHCR_USERNAME", "")
	t.Setenv("GHCR_TOKEN", "push-token")

	reg := v1alpha1.LocalRegistry{
		Registry: "${GHCR_USERNAME}:${GHCR_TOKEN}@ghcr.io/org/private-repo",
	}

	assert.False(t, reg.HasCredentials())
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

// A configured clusterTokenEnvVar sends cluster pull paths to a different environment
// variable than CLI push paths, which keep the password embedded in the Registry spec.
func TestLocalRegistry_ResolvePullCredentialsKeepsPushCredentialsSeparate(t *testing.T) {
	t.Setenv("GHCR_PULL_TOKEN", "pull-token")

	reg := v1alpha1.LocalRegistry{
		Registry: "user:push-token@ghcr.io/org/repo",
		//nolint:gosec // G101: these are environment variable names, not credentials.
		Credentials: v1alpha1.RegistryCredentials{
			ClusterTokenEnvVar: "GHCR_PULL_TOKEN",
		},
	}
	pushUsername, pushPassword := reg.ResolveCredentials()
	pullUsername, pullPassword := reg.ResolvePullCredentials()

	assert.Equal(t, "user", pushUsername)
	assert.Equal(t, "push-token", pushPassword)
	assert.Equal(t, "user", pullUsername)
	assert.Equal(t, "pull-token", pullPassword)
	assert.True(t, reg.UsesDedicatedPullCredentials())
}

// cliTokenEnvVar and clusterTokenEnvVar each override the shared tokenEnvVar for their
// own path only; an unset override falls back to tokenEnvVar.
func TestLocalRegistry_CredentialsOverridesApplyPerPath(t *testing.T) {
	t.Setenv("GHCR_TOKEN", "shared-token")
	t.Setenv("GHCR_PULL_TOKEN", "pull-token")

	shared := v1alpha1.LocalRegistry{
		Registry:    "user@ghcr.io/org/repo",
		Credentials: v1alpha1.RegistryCredentials{TokenEnvVar: "GHCR_TOKEN"},
	}
	_, sharedPush := shared.ResolveCredentials()
	_, sharedPull := shared.ResolvePullCredentials()

	assert.Equal(t, "shared-token", sharedPush)
	assert.Equal(t, "shared-token", sharedPull)
	assert.False(t, shared.UsesDedicatedPullCredentials())

	split := v1alpha1.LocalRegistry{
		Registry: "user@ghcr.io/org/repo",
		//nolint:gosec // G101: these are environment variable names, not credentials.
		Credentials: v1alpha1.RegistryCredentials{
			TokenEnvVar:        "GHCR_TOKEN",
			ClusterTokenEnvVar: "GHCR_PULL_TOKEN",
		},
	}
	_, splitPush := split.ResolveCredentials()
	_, splitPull := split.ResolvePullCredentials()

	assert.Equal(t, "shared-token", splitPush)
	assert.Equal(t, "pull-token", splitPull)
	assert.True(t, split.UsesDedicatedPullCredentials())
}

// A configured override stays authoritative even when its environment variable is
// missing or empty: resolution never silently falls back on process-environment state.
func TestLocalRegistry_ConfiguredEnvVarStaysAuthoritativeWhenUnset(t *testing.T) {
	t.Setenv("GHCR_TOKEN", "shared-token")
	t.Setenv("GHCR_PULL_TOKEN", "")

	reg := v1alpha1.LocalRegistry{
		Registry: "user:spec-token@ghcr.io/org/repo",
		//nolint:gosec // G101: these are environment variable names, not credentials.
		Credentials: v1alpha1.RegistryCredentials{
			TokenEnvVar:        "GHCR_TOKEN",
			ClusterTokenEnvVar: "GHCR_PULL_TOKEN",
		},
	}
	_, pullPassword := reg.ResolvePullCredentials()

	assert.Empty(t, pullPassword)
	assert.True(t, reg.UsesDedicatedPullCredentials())
}

// Credential resolution is registry-agnostic: no host has special meaning, so an
// ambient GHCR_PULL_TOKEN cannot leak into a registry that did not configure it.
func TestLocalRegistry_ResolvePullCredentialsIgnoresAmbientTokenWithoutConfiguredAuth(
	t *testing.T,
) {
	t.Setenv("GHCR_PULL_TOKEN", "ambient-pull-token")

	reg := v1alpha1.LocalRegistry{Registry: "ghcr.io/org/public-repo"}
	username, password := reg.ResolvePullCredentials()

	assert.Empty(t, username)
	assert.Empty(t, password)
	assert.False(t, reg.UsesDedicatedPullCredentials())
}

func TestLocalRegistry_ResolvePullCredentialsRejectsPasswordOnlyAuth(t *testing.T) {
	t.Setenv("GHCR_USERNAME", "")
	t.Setenv("GHCR_PULL_TOKEN", "pull-token")

	reg := v1alpha1.LocalRegistry{
		Registry: "${GHCR_USERNAME}:${GHCR_TOKEN}@ghcr.io/org/private-repo",
		//nolint:gosec // G101: this is an environment variable name, not a credential.
		Credentials: v1alpha1.RegistryCredentials{
			ClusterTokenEnvVar: "GHCR_PULL_TOKEN",
		},
	}
	username, password := reg.ResolvePullCredentials()

	assert.Empty(t, username)
	assert.Empty(t, password)
}

func TestLocalRegistry_ResolvePullCredentialsRejectsExpandedEmptyAuth(t *testing.T) {
	t.Setenv("GHCR_USERNAME", "")
	t.Setenv("GHCR_TOKEN", "")
	t.Setenv("GHCR_PULL_TOKEN", "ambient-pull-token")

	cluster := &v1alpha1.Cluster{}
	cluster.Spec.Cluster.LocalRegistry.Registry = "${GHCR_USERNAME}:${GHCR_TOKEN}@ghcr.io/org/private-repo"
	cluster.ExpandEnvVars()

	username, password := cluster.Spec.Cluster.LocalRegistry.ResolvePullCredentials()

	assert.Empty(t, username)
	assert.Empty(t, password)
}

func TestOptionsHetzner_PublicNetAccessors(t *testing.T) {
	t.Parallel()

	boolTrue := true
	boolFalse := false

	tests := []struct {
		name    string
		toggle  *bool
		enabled bool
	}{
		{name: "NilDefaultsToEnabled", toggle: nil, enabled: true},
		{name: "ExplicitTrueEnabled", toggle: &boolTrue, enabled: true},
		{name: "ExplicitFalseDisabled", toggle: &boolFalse, enabled: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := v1alpha1.OptionsHetzner{
				WorkerPublicIPv4:       testCase.toggle,
				WorkerPublicIPv6:       testCase.toggle,
				ControlPlanePublicIPv4: testCase.toggle,
				ControlPlanePublicIPv6: testCase.toggle,
			}

			assert.Equal(t, testCase.enabled, opts.WorkerIPv4Enabled())
			assert.Equal(t, testCase.enabled, opts.WorkerIPv6Enabled())
			assert.Equal(t, testCase.enabled, opts.ControlPlaneIPv4Enabled())
			assert.Equal(t, testCase.enabled, opts.ControlPlaneIPv6Enabled())
		})
	}
}
