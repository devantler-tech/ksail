package clusterprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSimpleDistributionConfig pins the name-only distribution mappings shared by the operator and
// the local `ksail open web` backend: K3s, VCluster, and KWOK are fully determined by the cluster name,
// while Vanilla, Talos, and EKS need caller-specific construction and must return nil.
//
//nolint:funlen // table-driven test with multiple test cases
func TestSimpleDistributionConfig(t *testing.T) {
	t.Parallel()

	const clusterName = "my-cluster"

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		clusterName  string
		assert       func(t *testing.T, cfg *clusterprovisioner.DistributionConfig)
	}{
		{
			name:         "k3s populates only the K3d config with name and k3d defaults",
			distribution: v1alpha1.DistributionK3s,
			clusterName:  clusterName,
			assert: func(t *testing.T, cfg *clusterprovisioner.DistributionConfig) {
				t.Helper()
				require.NotNil(t, cfg.K3d, "K3d config must be populated for K3s")
				assert.Equal(t, clusterName, cfg.K3d.Name)
				// The mapping passes empty apiVersion/kind, relying on NewK3dSimpleConfig's defaults.
				assert.Equal(t, "k3d.io/v1alpha5", cfg.K3d.APIVersion)
				assert.Equal(t, "Simple", cfg.K3d.Kind)
				assert.Nil(t, cfg.VCluster)
				assert.Nil(t, cfg.KWOK)
				assert.Nil(t, cfg.Kind)
				assert.Nil(t, cfg.Talos)
				assert.Nil(t, cfg.EKS)
			},
		},
		{
			name:         "k3s with empty name falls back to the k3d default name",
			distribution: v1alpha1.DistributionK3s,
			clusterName:  "",
			assert: func(t *testing.T, cfg *clusterprovisioner.DistributionConfig) {
				t.Helper()
				require.NotNil(t, cfg.K3d, "K3d config must be populated for K3s")
				assert.Equal(t, "k3d-default", cfg.K3d.Name)
			},
		},
		{
			name:         "vcluster populates only the VCluster config with the cluster name",
			distribution: v1alpha1.DistributionVCluster,
			clusterName:  clusterName,
			assert: func(t *testing.T, cfg *clusterprovisioner.DistributionConfig) {
				t.Helper()
				require.NotNil(t, cfg.VCluster, "VCluster config must be populated for VCluster")
				assert.Equal(t, clusterName, cfg.VCluster.Name)
				assert.Nil(t, cfg.K3d)
				assert.Nil(t, cfg.KWOK)
				assert.Nil(t, cfg.Kind)
				assert.Nil(t, cfg.Talos)
				assert.Nil(t, cfg.EKS)
			},
		},
		{
			name:         "kwok populates only the KWOK config with the cluster name",
			distribution: v1alpha1.DistributionKWOK,
			clusterName:  clusterName,
			assert: func(t *testing.T, cfg *clusterprovisioner.DistributionConfig) {
				t.Helper()
				require.NotNil(t, cfg.KWOK, "KWOK config must be populated for KWOK")
				assert.Equal(t, clusterName, cfg.KWOK.Name)
				assert.Nil(t, cfg.K3d)
				assert.Nil(t, cfg.VCluster)
				assert.Nil(t, cfg.Kind)
				assert.Nil(t, cfg.Talos)
				assert.Nil(t, cfg.EKS)
			},
		},
		{
			name:         "vanilla needs caller-specific construction so returns nil",
			distribution: v1alpha1.DistributionVanilla,
			clusterName:  clusterName,
			assert: func(t *testing.T, cfg *clusterprovisioner.DistributionConfig) {
				t.Helper()
				assert.Nil(t, cfg)
			},
		},
		{
			name:         "talos needs caller-specific construction so returns nil",
			distribution: v1alpha1.DistributionTalos,
			clusterName:  clusterName,
			assert: func(t *testing.T, cfg *clusterprovisioner.DistributionConfig) {
				t.Helper()
				assert.Nil(t, cfg)
			},
		},
		{
			name:         "eks needs caller-specific construction so returns nil",
			distribution: v1alpha1.DistributionEKS,
			clusterName:  clusterName,
			assert: func(t *testing.T, cfg *clusterprovisioner.DistributionConfig) {
				t.Helper()
				assert.Nil(t, cfg)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := clusterprovisioner.SimpleDistributionConfig(
				testCase.distribution,
				testCase.clusterName,
			)
			testCase.assert(t, cfg)
		})
	}
}
