package installer_test

import (
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail-go/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// assertTimeoutEquals is a helper that creates a cluster with the given timeout and asserts the result.
func assertTimeoutEquals(t *testing.T, clusterTimeout time.Duration, expected time.Duration) {
	t.Helper()

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Connection: v1alpha1.Connection{
				Timeout: metav1.Duration{Duration: clusterTimeout},
			},
		},
	}
	timeout := installer.GetInstallTimeout(cluster)
	assert.Equal(t, expected, timeout)
}

func TestGetInstallTimeout(t *testing.T) {
	t.Parallel()

	t.Run("returns_default_when_cluster_is_nil", func(t *testing.T) {
		t.Parallel()

		timeout := installer.GetInstallTimeout(nil)

		assert.Equal(t, installer.DefaultInstallTimeout, timeout)
	})

	t.Run("returns_default_when_timeout_is_zero", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 0, installer.DefaultInstallTimeout)
	})

	t.Run("returns_default_when_timeout_is_negative", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, -1*time.Minute, installer.DefaultInstallTimeout)
	})

	t.Run("returns_configured_timeout_when_set", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 10*time.Minute, 10*time.Minute)
	})

	t.Run("returns_configured_timeout_for_short_duration", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 30*time.Second, 30*time.Second)
	})

	t.Run("returns_configured_timeout_for_long_duration", func(t *testing.T) {
		t.Parallel()
		assertTimeoutEquals(t, 2*time.Hour, 2*time.Hour)
	})
}
