package localpathstorageinstaller

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestNewLocalPathStorageInstaller(t *testing.T) {
	t.Parallel()

	installer := NewLocalPathStorageInstaller(
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionKind,
	)

	assert.NotNil(t, installer)
	assert.Equal(t, "/path/to/kubeconfig", installer.kubeconfig)
	assert.Equal(t, "test-context", installer.context)
	assert.Equal(t, 5*time.Minute, installer.timeout)
	assert.Equal(t, v1alpha1.DistributionKind, installer.distribution)
}

func TestLocalPathStorageInstaller_Install_K3d(t *testing.T) {
	t.Parallel()

	// K3d already has local-path-provisioner, so Install should be a no-op
	installer := NewLocalPathStorageInstaller(
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionK3d,
	)

	ctx := context.Background()
	err := installer.Install(ctx)

	assert.NoError(t, err, "Install should succeed as no-op for K3d")
}

func TestLocalPathStorageInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	installer := NewLocalPathStorageInstaller(
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionKind,
	)

	ctx := context.Background()
	err := installer.Uninstall(ctx)

	assert.NoError(t, err, "Uninstall should always succeed as no-op")
}

func TestDistribution_ProvidesStorageByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     bool
	}{
		{
			name:         "K3d provides storage by default",
			distribution: v1alpha1.DistributionK3d,
			expected:     true,
		},
		{
			name:         "Kind does not provide storage by default",
			distribution: v1alpha1.DistributionKind,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.distribution.ProvidesStorageByDefault()
			assert.Equal(t, tt.expected, result)
		})
	}
}
