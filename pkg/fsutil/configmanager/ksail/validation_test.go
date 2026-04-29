package configmanager_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
)

func TestExpectedDistributionConfigName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         string
	}{
		{
			name:         "Vanilla",
			distribution: v1alpha1.DistributionVanilla,
			want:         "kind.yaml",
		},
		{
			name:         "K3s",
			distribution: v1alpha1.DistributionK3s,
			want:         "k3d.yaml",
		},
		{
			name:         "Talos",
			distribution: v1alpha1.DistributionTalos,
			want:         "talos",
		},
		{
			name:         "VCluster",
			distribution: v1alpha1.DistributionVCluster,
			want:         "vcluster.yaml",
		},
		{
			name:         "KWOK",
			distribution: v1alpha1.DistributionKWOK,
			want:         "kwok.yaml",
		},
		{
			name:         "EKS",
			distribution: v1alpha1.DistributionEKS,
			want:         "eks.yaml",
		},
		{
			name:         "Unknown",
			distribution: v1alpha1.Distribution(""),
			want:         "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := configmanager.ExpectedDistributionConfigNameForTest(tc.distribution)

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDistributionConfigIsOppositeDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		current      string
		distribution v1alpha1.Distribution
		want         bool
	}{
		{
			// "kind.yaml" is Vanilla's default — opposite of K3s
			name:         "kind.yaml_is_opposite_for_K3s",
			current:      "kind.yaml",
			distribution: v1alpha1.DistributionK3s,
			want:         true,
		},
		{
			// "kind.yaml" matches Vanilla — opposite of Talos
			name:         "kind.yaml_is_opposite_for_Talos",
			current:      "kind.yaml",
			distribution: v1alpha1.DistributionTalos,
			want:         true,
		},
		{
			// "kind.yaml" is the correct default for Vanilla — not opposite
			name:         "kind.yaml_is_not_opposite_for_Vanilla",
			current:      "kind.yaml",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
		},
		{
			// "k3d.yaml" is the correct default for K3s — not opposite
			name:         "k3d.yaml_is_not_opposite_for_K3s",
			current:      "k3d.yaml",
			distribution: v1alpha1.DistributionK3s,
			want:         false,
		},
		{
			// "k3d.yaml" is K3s's default — opposite of Vanilla
			name:         "k3d.yaml_is_opposite_for_Vanilla",
			current:      "k3d.yaml",
			distribution: v1alpha1.DistributionVanilla,
			want:         true,
		},
		{
			// "vcluster.yaml" is opposite for Vanilla
			name:         "vcluster.yaml_is_opposite_for_Vanilla",
			current:      "vcluster.yaml",
			distribution: v1alpha1.DistributionVanilla,
			want:         true,
		},
		{
			// "vcluster.yaml" is VCluster's default — not opposite
			name:         "vcluster.yaml_is_not_opposite_for_VCluster",
			current:      "vcluster.yaml",
			distribution: v1alpha1.DistributionVCluster,
			want:         false,
		},
		{
			// "talos" is Talos's default — not opposite
			name:         "talos_is_not_opposite_for_Talos",
			current:      "talos",
			distribution: v1alpha1.DistributionTalos,
			want:         false,
		},
		{
			// "talos" is Talos's default — opposite of Vanilla
			name:         "talos_is_opposite_for_Vanilla",
			current:      "talos",
			distribution: v1alpha1.DistributionVanilla,
			want:         true,
		},
		{
			// "kwok.yaml" is KWOK's default — not opposite
			name:         "kwok.yaml_is_not_opposite_for_KWOK",
			current:      "kwok.yaml",
			distribution: v1alpha1.DistributionKWOK,
			want:         false,
		},
		{
			// "kwok.yaml" is KWOK's default — opposite of Vanilla
			name:         "kwok.yaml_is_opposite_for_Vanilla",
			current:      "kwok.yaml",
			distribution: v1alpha1.DistributionVanilla,
			want:         true,
		},
		{
			// "eks.yaml" is EKS's default — not opposite
			name:         "eks.yaml_is_not_opposite_for_EKS",
			current:      "eks.yaml",
			distribution: v1alpha1.DistributionEKS,
			want:         false,
		},
		{
			// "eks.yaml" is EKS's default — opposite of Vanilla
			name:         "eks.yaml_is_opposite_for_Vanilla",
			current:      "eks.yaml",
			distribution: v1alpha1.DistributionVanilla,
			want:         true,
		},
		{
			// Custom file name — not any known default
			name:         "custom_config_is_not_opposite",
			current:      "my-custom-config.yaml",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
		},
		{
			// Empty current — not opposite
			name:         "empty_current_is_not_opposite",
			current:      "",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := configmanager.DistributionConfigIsOppositeDefaultForTest(tc.current, tc.distribution)

			assert.Equal(t, tc.want, got)
		})
	}
}
