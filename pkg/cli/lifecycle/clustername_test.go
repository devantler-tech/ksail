package lifecycle_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// vanillaCfg builds a minimal Vanilla cluster config with an optional context
// and metadata.name.
func vanillaCfg(contextName, metadataName string) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				Connection:   v1alpha1.Connection{Context: contextName},
			},
		},
	}
	cfg.Name = metadataName

	return cfg
}

// TestResolveClusterName_StandardPriority verifies the default
// context > metadata.name > distribution config ordering.
func TestResolveClusterName_StandardPriority(t *testing.T) {
	t.Parallel()

	kindCfg := &v1alpha4.Cluster{Name: "from-distconfig"}

	t.Run("context wins", func(t *testing.T) {
		t.Parallel()

		name, err := lifecycle.ResolveClusterName(
			vanillaCfg("kind-from-context", "from-metadata"), kindCfg,
		)
		require.NoError(t, err)
		assert.Equal(t, "from-context", name)
	})

	t.Run("metadata.name when no context", func(t *testing.T) {
		t.Parallel()

		name, err := lifecycle.ResolveClusterName(vanillaCfg("", "from-metadata"), kindCfg)
		require.NoError(t, err)
		assert.Equal(t, "from-metadata", name)
	})

	t.Run("distribution config when neither", func(t *testing.T) {
		t.Parallel()

		name, err := lifecycle.ResolveClusterName(vanillaCfg("", ""), kindCfg)
		require.NoError(t, err)
		assert.Equal(t, "from-distconfig", name)
	})
}

// TestResolveClusterName_DistConfigError verifies that a configmanager error is
// surfaced when there is no fallback.
func TestResolveClusterName_DistConfigError(t *testing.T) {
	t.Parallel()

	// An unsupported distConfig type makes configmanager.GetClusterName fail.
	_, err := lifecycle.ResolveClusterName(vanillaCfg("", ""), struct{}{})
	require.Error(t, err)
}

// TestResolveClusterName_DistConfigPriority verifies the Omni-specific ordering:
// distribution config first, metadata.name/context skipped, and a fallback used
// only when the distribution config yields nothing.
func TestResolveClusterName_DistConfigPriority(t *testing.T) {
	t.Parallel()

	t.Run("distconfig wins over fallback and is preferred over context", func(t *testing.T) {
		t.Parallel()

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionTalos,
					Connection:   v1alpha1.Connection{Context: "admin@should-be-ignored"},
				},
			},
		}
		cfg.Name = "should-be-ignored-too"

		talosCfg := &talosconfigmanager.Configs{Name: "from-talos"}

		name, err := lifecycle.ResolveClusterName(
			cfg,
			talosCfg,
			lifecycle.WithDistConfigPriority(),
			lifecycle.WithClusterNameFallback(func() string { return "fallback" }),
		)
		require.NoError(t, err)
		assert.Equal(t, "from-talos", name)
	})

	t.Run("fallback used when distconfig empty", func(t *testing.T) {
		t.Parallel()

		name, err := lifecycle.ResolveClusterName(
			nil,
			nil,
			lifecycle.WithDistConfigPriority(),
			lifecycle.WithClusterNameFallback(func() string { return "fallback" }),
		)
		require.NoError(t, err)
		assert.Equal(t, "fallback", name)
	})
}

// TestClusterNameFromDistributionConfig verifies the shared distribution-config
// name extraction across all distribution types.
func TestClusterNameFromDistributionConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		distCfg *clusterprovisioner.DistributionConfig
		want    string
	}{
		{"nil", nil, ""},
		{"empty struct", &clusterprovisioner.DistributionConfig{}, ""},
		{
			"kind",
			&clusterprovisioner.DistributionConfig{Kind: &v1alpha4.Cluster{Name: "k"}},
			"k",
		},
		{
			"talos",
			&clusterprovisioner.DistributionConfig{
				Talos: &talosconfigmanager.Configs{Name: "t"},
			},
			"t",
		},
		{
			"vcluster",
			&clusterprovisioner.DistributionConfig{
				VCluster: &clusterprovisioner.VClusterConfig{Name: "v"},
			},
			"v",
		},
		{
			"kwok",
			&clusterprovisioner.DistributionConfig{
				KWOK: &clusterprovisioner.KWOKConfig{Name: "w"},
			},
			"w",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := lifecycle.ClusterNameFromDistributionConfig(testCase.distCfg)
			assert.Equal(t, testCase.want, got)
		})
	}
}
