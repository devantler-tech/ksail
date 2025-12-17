package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestDistribution_ProvidesMetricsServerByDefault(t *testing.T) {
	t.Parallel()

	t.Run("returns_true_for_k3d", func(t *testing.T) {
		t.Parallel()

		dist := v1alpha1.DistributionK3d

		result := dist.ProvidesMetricsServerByDefault()

		assert.True(t, result, "K3d should provide metrics-server by default")
	})

	t.Run("returns_false_for_kind", func(t *testing.T) {
		t.Parallel()

		dist := v1alpha1.DistributionKind

		result := dist.ProvidesMetricsServerByDefault()

		assert.False(t, result, "Kind should not provide metrics-server by default")
	})

	t.Run("returns_false_for_unknown_distribution", func(t *testing.T) {
		t.Parallel()

		dist := v1alpha1.Distribution("unknown")

		result := dist.ProvidesMetricsServerByDefault()

		assert.False(
			t,
			result,
			"Unknown distributions should not provide metrics-server by default",
		)
	})

	t.Run("returns_false_for_empty_distribution", func(t *testing.T) {
		t.Parallel()

		dist := v1alpha1.Distribution("")

		result := dist.ProvidesMetricsServerByDefault()

		assert.False(t, result, "Empty distribution should not provide metrics-server by default")
	})
}
