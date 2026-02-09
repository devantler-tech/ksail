package localpathstorageinstaller_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/localpathstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		kubeconfig   string
		context      string
		timeout      time.Duration
		distribution v1alpha1.Distribution
	}{
		{
			name:         "vanilla distribution",
			kubeconfig:   "/path/to/kubeconfig",
			context:      "test-context",
			timeout:      5 * time.Minute,
			distribution: v1alpha1.DistributionVanilla,
		},
		{
			name:         "k3s distribution",
			kubeconfig:   "",
			context:      "",
			timeout:      1 * time.Minute,
			distribution: v1alpha1.DistributionK3s,
		},
		{
			name:         "talos distribution",
			kubeconfig:   "/custom/kubeconfig",
			context:      "my-context",
			timeout:      10 * time.Minute,
			distribution: v1alpha1.DistributionTalos,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			installer := localpathstorageinstaller.NewInstaller(
				testCase.kubeconfig,
				testCase.context,
				testCase.timeout,
				testCase.distribution,
			)

			assert.NotNil(t, installer)
		})
	}
}

func TestInstaller_Install(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expectNoOp   bool
	}{
		{
			name:         "k3s distribution is no-op",
			distribution: v1alpha1.DistributionK3s,
			expectNoOp:   true,
		},
		{
			name:         "vanilla distribution needs installation",
			distribution: v1alpha1.DistributionVanilla,
			expectNoOp:   false,
		},
		{
			name:         "talos distribution needs installation",
			distribution: v1alpha1.DistributionTalos,
			expectNoOp:   false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			installer := localpathstorageinstaller.NewInstaller(
				"/path/to/kubeconfig",
				"test-context",
				5*time.Minute,
				testCase.distribution,
			)

			ctx := context.Background()
			err := installer.Install(ctx)

			if testCase.expectNoOp {
				assert.NoError(t, err, "Install should succeed as no-op")
			}
		})
	}
}

func TestInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	distributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}

	for _, dist := range distributions {
		t.Run(string(dist), func(t *testing.T) {
			t.Parallel()

			installer := localpathstorageinstaller.NewInstaller(
				"/path/to/kubeconfig",
				"test-context",
				5*time.Minute,
				dist,
			)

			ctx := context.Background()
			err := installer.Uninstall(ctx)

			require.NoError(t, err, "Uninstall should always succeed as no-op")
		})
	}
}

func TestInstaller_Images_K3s(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionK3s,
	)

	ctx := context.Background()
	images, err := installer.Images(ctx)

	require.NoError(t, err)
	assert.Empty(t, images, "K3s should return empty image list")
}

func TestInstaller_Images_Success(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		30*time.Second,
		v1alpha1.DistributionVanilla,
	)

	ctx := context.Background()
	images, err := installer.Images(ctx)

	require.NoError(t, err)
	assert.NotEmpty(t, images, "Should extract images from real manifest")
	// The real manifest should contain the local-path-provisioner image
	foundProvisionerImage := false

	for _, img := range images {
		if strings.Contains(img, "local-path-provisioner") {
			foundProvisionerImage = true

			break
		}
	}

	assert.True(t, foundProvisionerImage, "Should find local-path-provisioner image in manifest")
}

func TestInstaller_Images_Talos(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		30*time.Second,
		v1alpha1.DistributionTalos,
	)

	ctx := context.Background()
	images, err := installer.Images(ctx)

	require.NoError(t, err)
	assert.NotEmpty(t, images, "Talos should also fetch images from manifest")
}

func TestInstaller_Images_ShortTimeout(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		1*time.Nanosecond,
		v1alpha1.DistributionVanilla,
	)

	ctx := context.Background()
	images, err := installer.Images(ctx)

	require.Error(t, err, "Very short timeout should cause error")
	assert.Empty(t, images)
}

func TestInstaller_Images_CanceledContext(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		30*time.Second,
		v1alpha1.DistributionVanilla,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	images, err := installer.Images(ctx)

	require.Error(t, err)
	assert.Empty(t, images)
}
