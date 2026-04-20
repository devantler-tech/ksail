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

func TestInstaller_Install_DeadlineExceeded_NonK3s(t *testing.T) {
	t.Parallel()

	// Use an already-expired deadline context (different from context.Cancel)
	// on a Vanilla distribution to exercise the installLocalPathProvisioner path.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	installer := localpathstorageinstaller.NewInstaller(
		"/nonexistent/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	err := installer.Install(ctx)
	require.Error(t, err)
}

func TestInstaller_Install_TalosDistribution(t *testing.T) {
	t.Parallel()

	// Talos also needs local-path-provisioner (not K3s/VCluster).
	// With invalid kubeconfig, Install should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	installer := localpathstorageinstaller.NewInstaller(
		"/nonexistent/kubeconfig",
		"test-context",
		2*time.Second,
		v1alpha1.DistributionTalos,
	)

	err := installer.Install(ctx)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "K3s") // should not mention K3s since we're using Talos
}
