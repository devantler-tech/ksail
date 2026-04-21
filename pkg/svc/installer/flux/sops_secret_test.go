package fluxinstaller_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestExtractAgeKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single key line",
			input: "AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			want:  "AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01",
		},
		{
			name: "key with metadata comments",
			input: `# created: 2024-01-01T00:00:00Z
# public key: age1abc123
AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01
`,
			want: "AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01",
		},
		{
			name: "multiple keys returns first",
			input: `AGE-SECRET-KEY-FIRST00000000000000000000000000000000000000000000000000
AGE-SECRET-KEY-SECOND0000000000000000000000000000000000000000000000000`,
			want: "AGE-SECRET-KEY-FIRST00000000000000000000000000000000000000000000000000",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no age key",
			input: "some random data\nno age key here",
			want:  "",
		},
		{
			name:  "key with surrounding whitespace",
			input: "  AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01  ",
			want:  "AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := fluxinstaller.ExtractAgeKey(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveAgeKey(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1TESTKEY000000000000000000000000000000000000000000000000"

	t.Run("env var set with valid key", func(t *testing.T) {
		t.Setenv("TEST_SOPS_AGE_KEY_RESOLVE", testKey)

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPS_AGE_KEY_RESOLVE"}

		got, err := fluxinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, testKey, got)
	})

	t.Run("env var not set and no key file returns empty", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-test-keys.txt")

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPS_NONEXISTENT_VAR_12345"}

		got, err := fluxinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("env var name empty skips env lookup", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-test-keys-empty-var.txt")

		sops := v1alpha1.SOPS{AgeKeyEnvVar: ""}

		got, err := fluxinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("env var with metadata extracts key", func(t *testing.T) {
		const metaKey = "AGE-SECRET-KEY-1METAKEY000000000000000000000000000000000000000000000000"
		t.Setenv("TEST_SOPS_AGE_KEY_META", "# comment\n"+metaKey+"\n")

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPS_AGE_KEY_META"}

		got, err := fluxinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, metaKey, got)
	})
}

func TestBuildSopsAgeSecret(t *testing.T) {
	t.Parallel()

	ageKey := "AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	secret := fluxinstaller.BuildSopsAgeSecret(ageKey)

	require.NotNil(t, secret)
	assert.Equal(t, fluxinstaller.SopsAgeSecretName, secret.Name)
	assert.Equal(t, "flux-system", secret.Namespace)
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
	assert.Equal(t, "ksail", secret.Labels["app.kubernetes.io/managed-by"])

	require.Contains(t, secret.Data, "sops.agekey")
	assert.Equal(t, []byte(ageKey), secret.Data["sops.agekey"])
}

func TestEnsureSopsAgeSecret_Disabled(t *testing.T) {
	t.Parallel()

	disabled := false
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					Enabled: &disabled,
				},
			},
		},
	}

	// Should return nil (no-op) even with nil restConfig since it short-circuits
	err := fluxinstaller.EnsureSopsAgeSecret(t.Context(), nil, clusterCfg)
	require.NoError(t, err)
}

func TestEnsureSopsAgeSecret_NoKeyAutoDetect(t *testing.T) {
	// Prevent local key file fallback
	t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-test-keys.txt")

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					AgeKeyEnvVar: "TEST_SOPS_NONEXISTENT_VAR_99999",
				},
			},
		},
	}

	// Auto-detect mode with no key available: should skip silently
	err := fluxinstaller.EnsureSopsAgeSecret(t.Context(), nil, clusterCfg)
	require.NoError(t, err)
}

func TestEnsureSopsAgeSecret_EnabledNoKey(t *testing.T) {
	// Prevent local key file fallback
	t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-test-keys.txt")

	enabled := true
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					Enabled:      &enabled,
					AgeKeyEnvVar: "TEST_SOPS_NONEXISTENT_VAR_88888",
				},
			},
		},
	}

	// Explicitly enabled but no key: should error
	err := fluxinstaller.EnsureSopsAgeSecret(t.Context(), nil, clusterCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SOPS is enabled but no Age key found")
}
