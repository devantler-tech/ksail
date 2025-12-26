package installer_test

import (
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// assertTimeoutEquals is a helper that creates a cluster with the given timeout and asserts the result.
func assertTimeoutEquals(t *testing.T, clusterTimeout time.Duration, expected time.Duration) {
	t.Helper()

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{
					Timeout: metav1.Duration{Duration: clusterTimeout},
				},
			},
		},
	}
	timeout := installer.GetInstallTimeout(cluster)
	assert.Equal(t, expected, timeout)
}

// assertTimeoutEqualsWithDistribution is a helper that creates a cluster with
// the given distribution and timeout and asserts the result.
func assertTimeoutEqualsWithDistribution(
	t *testing.T,
	distribution v1alpha1.Distribution,
	clusterTimeout time.Duration,
	expected time.Duration,
) {
	t.Helper()

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: distribution,
				Connection: v1alpha1.Connection{
					Timeout: metav1.Duration{Duration: clusterTimeout},
				},
			},
		},
	}
	timeout := installer.GetInstallTimeout(cluster)
	assert.Equal(t, expected, timeout)
}

func TestGetInstallTimeout(t *testing.T) {
	t.Parallel()

	t.Run("nil_cluster", func(t *testing.T) {
		t.Parallel()

		timeout := installer.GetInstallTimeout(nil)

		assert.Equal(t, installer.DefaultInstallTimeout, timeout)
	})

	t.Run("zero_timeout", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 0, installer.DefaultInstallTimeout)
	})

	t.Run("negative_timeout", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, -1*time.Minute, installer.DefaultInstallTimeout)
	})

	t.Run("explicit_timeout", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 10*time.Minute, 10*time.Minute)
	})

	t.Run("short_duration", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 30*time.Second, 30*time.Second)
	})

	t.Run("long_duration", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 2*time.Hour, 2*time.Hour)
	})
}

func TestGetInstallTimeoutDistributions(t *testing.T) {
	t.Parallel()

	t.Run("talos_default", func(t *testing.T) {
		t.Parallel()

		assertTimeoutEqualsWithDistribution(
			t, v1alpha1.DistributionTalosInDocker, 0, installer.TalosInstallTimeout,
		)
	})

	t.Run("talos_explicit", func(t *testing.T) {
		t.Parallel()

		assertTimeoutEqualsWithDistribution(
			t, v1alpha1.DistributionTalosInDocker, 15*time.Minute, 15*time.Minute,
		)
	})

	t.Run("kind_default", func(t *testing.T) {
		t.Parallel()

		assertTimeoutEqualsWithDistribution(
			t, v1alpha1.DistributionKind, 0, installer.DefaultInstallTimeout,
		)
	})

	t.Run("k3d_default", func(t *testing.T) {
		t.Parallel()

		assertTimeoutEqualsWithDistribution(
			t, v1alpha1.DistributionK3d, 0, installer.DefaultInstallTimeout,
		)
	})
}
