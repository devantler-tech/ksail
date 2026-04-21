package argocdinstaller_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestExtractAgeKey verifies key extraction from various input formats.
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := argocdinstaller.ExtractAgeKey(testCase.input)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestResolveAgeKey verifies Age key resolution from env vars and key files.
func TestResolveAgeKey(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1TESTKEY000000000000000000000000000000000000000000000000"

	t.Run("env var set with valid key", func(t *testing.T) {
		t.Setenv("TEST_ARGOCD_SOPS_AGE_KEY_RESOLVE", testKey)

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_ARGOCD_SOPS_AGE_KEY_RESOLVE"}

		got, err := argocdinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, testKey, got)
	})

	t.Run("env var not set and no key file returns empty", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-argocd-test-keys.txt")

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_ARGOCD_SOPS_NONEXISTENT_VAR_12345"}

		got, err := argocdinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("env var name empty skips env lookup", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-argocd-test-empty.txt")

		sops := v1alpha1.SOPS{AgeKeyEnvVar: ""}

		got, err := argocdinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("env var with metadata extracts key", func(t *testing.T) {
		const metaKey = "AGE-SECRET-KEY-1METAKEY000000000000000000000000000000000000000000000000"
		t.Setenv("TEST_ARGOCD_SOPS_AGE_KEY_META", "# comment\n"+metaKey+"\n")

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_ARGOCD_SOPS_AGE_KEY_META"}

		got, err := argocdinstaller.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, metaKey, got)
	})
}

// TestBuildSopsAgeSecret verifies the secret is constructed correctly for the argocd namespace.
func TestBuildSopsAgeSecret(t *testing.T) {
	t.Parallel()

	ageKey := "AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	secret := argocdinstaller.BuildSopsAgeSecret(ageKey)

	require.NotNil(t, secret)
	assert.Equal(t, argocdinstaller.SopsAgeSecretName, secret.Name)
	assert.Equal(t, "argocd", secret.Namespace)
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
	assert.Equal(t, "ksail", secret.Labels["app.kubernetes.io/managed-by"])

	require.Contains(t, secret.Data, "sops.agekey")
	assert.Equal(t, []byte(ageKey), secret.Data["sops.agekey"])
}

// TestEnsureSopsAgeSecret_Disabled verifies no-op when SOPS is explicitly disabled.
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

	// Should return nil (no-op) before needing kubeconfig since SOPS is disabled
	err := argocdinstaller.EnsureSopsAgeSecret(t.Context(), "", clusterCfg)
	require.NoError(t, err)
}

// TestEnsureSopsAgeSecret_NoKeyAutoDetect verifies silent skip in auto-detect mode with no key.
func TestEnsureSopsAgeSecret_NoKeyAutoDetect(t *testing.T) {
	// Prevent local key file fallback
	t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-argocd-test-autodetect.txt")

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					AgeKeyEnvVar: "TEST_ARGOCD_SOPS_NONEXISTENT_VAR_99999",
				},
			},
		},
	}

	// Auto-detect mode with no key available: should skip silently (no kubeconfig needed)
	err := argocdinstaller.EnsureSopsAgeSecret(t.Context(), "", clusterCfg)
	require.NoError(t, err)
}

// TestEnsureSopsAgeSecret_EnabledNoKey verifies an error is returned when SOPS is required but no key found.
func TestEnsureSopsAgeSecret_EnabledNoKey(t *testing.T) {
	// Prevent local key file fallback
	t.Setenv("SOPS_AGE_KEY_FILE", "/tmp/nonexistent-ksail-argocd-test-enabled.txt")

	enabled := true
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					Enabled:      &enabled,
					AgeKeyEnvVar: "TEST_ARGOCD_SOPS_NONEXISTENT_VAR_88888",
				},
			},
		},
	}

	// Explicitly enabled but no key: should error
	err := argocdinstaller.EnsureSopsAgeSecret(t.Context(), "", clusterCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SOPS is enabled but no Age key found")
}

// TestUpsertSopsAgeSecret_Create verifies secret creation when none exists.
func TestUpsertSopsAgeSecret_Create(t *testing.T) {
	t.Parallel()

	const ageKey = "AGE-SECRET-KEY-1ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF01"

	clientset := fake.NewSimpleClientset()

	err := argocdinstaller.UpsertSopsAgeSecret(context.Background(), clientset, ageKey)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("argocd").Get(
		context.Background(), argocdinstaller.SopsAgeSecretName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, []byte(ageKey), secret.Data["sops.agekey"])
	assert.Equal(t, "ksail", secret.Labels["app.kubernetes.io/managed-by"])
}

// TestUpsertSopsAgeSecret_Update verifies secret is updated when it already exists.
func TestUpsertSopsAgeSecret_Update(t *testing.T) {
	t.Parallel()

	const (
		oldKey = "AGE-SECRET-KEY-1OLDKEY0000000000000000000000000000000000000000000000000"
		newKey = "AGE-SECRET-KEY-1NEWKEY0000000000000000000000000000000000000000000000000"
	)

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      argocdinstaller.SopsAgeSecretName,
			Namespace: "argocd",
		},
		Data: map[string][]byte{
			"sops.agekey": []byte(oldKey),
		},
	}

	clientset := fake.NewSimpleClientset(existing)

	err := argocdinstaller.UpsertSopsAgeSecret(context.Background(), clientset, newKey)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("argocd").Get(
		context.Background(), argocdinstaller.SopsAgeSecretName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, []byte(newKey), secret.Data["sops.agekey"])
}
