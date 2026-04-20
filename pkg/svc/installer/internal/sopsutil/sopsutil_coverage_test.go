package sopsutil_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// ---------------------------------------------------------------------------
// BuildSopsAgeSecret
// ---------------------------------------------------------------------------

func TestBuildSopsAgeSecret(t *testing.T) {
	t.Parallel()

	const (
		ageKey    = "AGE-SECRET-KEY-1TESTKEY000000000000000000000000000000000000000000000000"
		namespace = "flux-system"
	)

	secret := sopsutil.BuildSopsAgeSecret(namespace, ageKey)

	assert.Equal(t, "sops-age", secret.Name)
	assert.Equal(t, namespace, secret.Namespace)
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
	assert.Equal(t, map[string]string{"app.kubernetes.io/managed-by": "ksail"}, secret.Labels)
	require.Contains(t, secret.Data, "sops.agekey")
	assert.Equal(t, []byte(ageKey), secret.Data["sops.agekey"])
}

func TestBuildSopsAgeSecret_EmptyKey(t *testing.T) {
	t.Parallel()

	secret := sopsutil.BuildSopsAgeSecret("default", "")

	assert.Equal(t, "sops-age", secret.Name)
	assert.Equal(t, "default", secret.Namespace)
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
	require.Contains(t, secret.Data, "sops.agekey")
	assert.Equal(t, []byte(""), secret.Data["sops.agekey"])
}

func TestBuildSopsAgeSecret_DifferentNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
	}{
		{name: "flux-system", namespace: "flux-system"},
		{name: "argocd", namespace: "argocd"},
		{name: "custom", namespace: "my-namespace"},
		{name: "empty", namespace: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			secret := sopsutil.BuildSopsAgeSecret(tc.namespace, "AGE-SECRET-KEY-DUMMY")
			assert.Equal(t, tc.namespace, secret.Namespace)
			assert.Equal(t, "sops-age", secret.Name)
		})
	}
}

// ---------------------------------------------------------------------------
// ResolveAgeKey
// ---------------------------------------------------------------------------

func noKeyFile(t *testing.T) {
	t.Helper()
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "nonexistent-keys.txt"))
}

//nolint:paralleltest // Uses t.Setenv
func TestResolveAgeKey_EmptyEnvVarName(t *testing.T) {
	noKeyFile(t)

	sops := v1alpha1.SOPS{AgeKeyEnvVar: ""}
	key, err := sopsutil.ResolveAgeKey(sops)

	require.NoError(t, err)
	assert.Empty(
		t,
		key,
		"empty env var name should skip env lookup and return empty when no key file",
	)
}

func TestResolveAgeKey_EnvVarSetWithoutAgePrefix(t *testing.T) {
	t.Setenv("TEST_SOPSUTIL_NO_PREFIX_88888", "not-a-valid-age-key")
	noKeyFile(t)

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_NO_PREFIX_88888"}
	key, err := sopsutil.ResolveAgeKey(sops)

	require.NoError(t, err)
	assert.Empty(t, key, "value without AGE-SECRET-KEY- prefix should not be extracted")
}

// ---------------------------------------------------------------------------
// ResolveEnabledAgeKey
// ---------------------------------------------------------------------------

func TestResolveEnabledAgeKey_NilEnabled(t *testing.T) {
	t.Setenv("TEST_SOPSUTIL_NILCHECK_99999", "")
	noKeyFile(t)

	sops := v1alpha1.SOPS{
		AgeKeyEnvVar: "TEST_SOPSUTIL_NILCHECK_99999",
		Enabled:      nil,
	}
	key, err := sopsutil.ResolveEnabledAgeKey(sops)

	require.NoError(t, err)
	assert.Empty(t, key, "nil Enabled (auto-detect) with no key should return empty without error")
}

// ---------------------------------------------------------------------------
// AgeSecretKeyPrefix constant
// ---------------------------------------------------------------------------

func TestAgeSecretKeyPrefix_Constant(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "AGE-SECRET-KEY-", sopsutil.AgeSecretKeyPrefix)
}
