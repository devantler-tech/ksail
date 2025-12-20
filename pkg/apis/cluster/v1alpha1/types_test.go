package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestDistribution_ProvidesMetricsServerByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         bool
		description  string
	}{
		{
			name:         "returns_true_for_k3d",
			distribution: v1alpha1.DistributionK3d,
			want:         true,
			description:  "K3d should provide metrics-server by default",
		},
		{
			name:         "returns_false_for_kind",
			distribution: v1alpha1.DistributionKind,
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.distribution.ProvidesMetricsServerByDefault()

			assert.Equal(t, tt.want, result, tt.description)
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
			name:         "returns_true_for_k3d",
			distribution: v1alpha1.DistributionK3d,
			want:         true,
			description:  "K3d should provide storage by default",
		},
		{
			name:         "returns_false_for_kind",
			distribution: v1alpha1.DistributionKind,
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.distribution.ProvidesStorageByDefault()

			assert.Equal(t, tt.want, result, tt.description)
		})
	}
}
