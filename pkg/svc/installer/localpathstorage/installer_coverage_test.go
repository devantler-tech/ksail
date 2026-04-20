package localpathstorageinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/localpathstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstaller_Install_VClusterDistribution(t *testing.T) {
	// VCluster doesn't provide storage by default, so Install attempts
	// local-path-provisioner installation — which will fail without a cluster.
	installer := localpathstorageinstaller.NewInstaller(
		"/nonexistent/kubeconfig",
		"nonexistent-ctx",
		5*time.Second,
		v1alpha1.DistributionVCluster,
	)

	t.Setenv("PATH", "")

	err := installer.Install(context.Background())
	require.Error(t, err, "VCluster install should attempt provisioner and fail without a cluster")
}

func TestInstaller_Images_VClusterDistribution(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		30*time.Second,
		v1alpha1.DistributionVCluster,
	)

	images, err := installer.Images(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, images, "VCluster should fetch images (same as Vanilla)")
}

func TestNewInstaller_ZeroTimeout(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"ctx",
		0,
		v1alpha1.DistributionVanilla,
	)
	require.NotNil(t, installer)
}

func TestInstaller_Uninstall_CanceledContext(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := installer.Uninstall(ctx)
	require.NoError(t, err, "Uninstall is a no-op and should succeed even with canceled context")
}

func TestInstaller_Images_CanceledContext_VCluster(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		30*time.Second,
		v1alpha1.DistributionVCluster,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	images, err := installer.Images(ctx)
	require.Error(t, err, "Canceled context should cause HTTP fetch failure for VCluster")
	assert.Empty(t, images)
}

func TestInstaller_Install_CanceledContext(t *testing.T) {
	// Non-K3s distribution with canceled context should fail.
	installer := localpathstorageinstaller.NewInstaller(
		"/nonexistent/kubeconfig",
		"nonexistent-ctx",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Setenv("PATH", "")

	err := installer.Install(ctx)
	require.Error(t, err, "Install with canceled context should fail")
}
