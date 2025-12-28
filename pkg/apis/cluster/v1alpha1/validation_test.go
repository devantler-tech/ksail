package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributionSet_AcceptsTalos(t *testing.T) {
	t.Parallel()

	var dist v1alpha1.Distribution
	require.NoError(t, dist.Set("Talos"))
	assert.Equal(t, v1alpha1.DistributionTalos, dist)
}

func TestDistributionSet_CaseInsensitive(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		expected v1alpha1.Distribution
	}{
		{"talosindocker", v1alpha1.DistributionTalos},
		{"TALOSINDOCKER", v1alpha1.DistributionTalos},
		{"Talos", v1alpha1.DistributionTalos},
	}

	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			var dist v1alpha1.Distribution
			require.NoError(t, dist.Set(testCase.input))
			assert.Equal(t, testCase.expected, dist)
		})
	}
}

func TestDistributionSet_InvalidListsValidOptions(t *testing.T) {
	t.Parallel()

	var dist v1alpha1.Distribution

	err := dist.Set("invalid")
	require.Error(t, err)

	require.ErrorIs(t, err, v1alpha1.ErrInvalidDistribution)
	assert.Contains(t, err.Error(), "Kind")
	assert.Contains(t, err.Error(), "K3d")
	assert.Contains(t, err.Error(), "Talos")
}

func TestValidDistributions_IncludesTalos(t *testing.T) {
	t.Parallel()

	distributions := v1alpha1.ValidDistributions()
	assert.Contains(t, distributions, v1alpha1.DistributionTalos)
	assert.Len(t, distributions, 3) // Kind, K3d, Talos
}

func TestTalosProvidesMetricsServerByDefault_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionTalos
	assert.False(t, dist.ProvidesMetricsServerByDefault())
}

func TestTalosProvidesStorageByDefault_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionTalos
	assert.False(t, dist.ProvidesStorageByDefault())
}

func TestGitOpsEngineSet_AcceptsArgoCD(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine
	require.NoError(t, engine.Set("ArgoCD"))
	assert.Equal(t, v1alpha1.GitOpsEngine("ArgoCD"), engine)
}

func TestGitOpsEngineSet_InvalidListsValidOptions(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine

	err := engine.Set("invalid")
	require.Error(t, err)

	require.ErrorIs(t, err, v1alpha1.ErrInvalidGitOpsEngine)
	assert.Contains(t, err.Error(), "None")
	assert.Contains(t, err.Error(), "Flux")
	assert.Contains(t, err.Error(), "ArgoCD")
}
