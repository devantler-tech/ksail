package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributionSet_AcceptsTalosInDocker(t *testing.T) {
	t.Parallel()

	var dist v1alpha1.Distribution
	require.NoError(t, dist.Set("TalosInDocker"))
	assert.Equal(t, v1alpha1.DistributionTalosInDocker, dist)
}

func TestDistributionSet_CaseInsensitive(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		expected v1alpha1.Distribution
	}{
		{"talosindocker", v1alpha1.DistributionTalosInDocker},
		{"TALOSINDOCKER", v1alpha1.DistributionTalosInDocker},
		{"TalosInDocker", v1alpha1.DistributionTalosInDocker},
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
	assert.Contains(t, err.Error(), "TalosInDocker")
}

func TestValidDistributions_IncludesTalosInDocker(t *testing.T) {
	t.Parallel()

	distributions := v1alpha1.ValidDistributions()
	assert.Contains(t, distributions, v1alpha1.DistributionTalosInDocker)
	assert.Len(t, distributions, 3) // Kind, K3d, TalosInDocker
}

func TestTalosInDockerProvidesMetricsServerByDefault_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionTalosInDocker
	assert.False(t, dist.ProvidesMetricsServerByDefault())
}

func TestTalosInDockerProvidesStorageByDefault_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionTalosInDocker
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
