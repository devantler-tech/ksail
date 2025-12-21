package localpathstorageinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/localpathstorage"
	"github.com/stretchr/testify/assert"
)

func TestNewLocalPathStorageInstaller(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewLocalPathStorageInstaller(
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionKind,
	)

	assert.NotNil(t, installer)
}

func TestLocalPathStorageInstaller_Install_K3d(t *testing.T) {
	t.Parallel()

	// K3d already has local-path-provisioner, so Install should be a no-op
	installer := localpathstorageinstaller.NewLocalPathStorageInstaller(
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

	installer := localpathstorageinstaller.NewLocalPathStorageInstaller(
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionKind,
	)

	ctx := context.Background()
	err := installer.Uninstall(ctx)

	assert.NoError(t, err, "Uninstall should always succeed as no-op")
}
