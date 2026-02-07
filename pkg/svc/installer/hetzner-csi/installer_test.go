package hetznercsiinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/hetzner-csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewHetznerCSIInstaller(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	assert.NotNil(t, installer)
}

func TestHetznerCSIInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	t.Run("successfully uninstalls release", func(t *testing.T) {
		t.Parallel()

		mockClient := helm.NewMockInterface(t)
		timeout := 5 * time.Minute

		mockClient.EXPECT().
			UninstallRelease(mock.Anything, "hcloud-csi", "kube-system").
			Return(nil).
			Once()

		installer := hetznercsiinstaller.NewHetznerCSIInstaller(
			mockClient,
			"~/.kube/config",
			"test-context",
			timeout,
		)
		err := installer.Uninstall(context.Background())

		require.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("returns error when uninstall fails", func(t *testing.T) {
		t.Parallel()

		mockClient := helm.NewMockInterface(t)
		timeout := 5 * time.Minute

		mockClient.EXPECT().
			UninstallRelease(mock.Anything, "hcloud-csi", "kube-system").
			Return(assert.AnError).
			Once()

		installer := hetznercsiinstaller.NewHetznerCSIInstaller(
			mockClient,
			"~/.kube/config",
			"test-context",
			timeout,
		)
		err := installer.Uninstall(context.Background())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to uninstall hetzner-csi release")
		mockClient.AssertExpectations(t)
	})
}

func TestHetznerCSIInstaller_Install_MissingToken(t *testing.T) {
	// Do not call t.Parallel(): t.Setenv mutates process-wide state
	// and is incompatible with parallel test execution.
	t.Setenv("HCLOUD_TOKEN", "")

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	err := installer.Install(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, hetznercsiinstaller.ErrHetznerTokenNotSet)
	assert.Contains(t, err.Error(), "HCLOUD_TOKEN")
}

func TestHetznerCSIInstaller_Images_SuccessfulExtraction(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	// Mock manifest with container images
	mockManifest := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: csi-driver
    image: hetznercloud/hcloud-csi-driver:v2.5.0
  - name: csi-attacher
    image: registry.k8s.io/sig-storage/csi-attacher:v4.3.0
`

	mockClient.EXPECT().
		TemplateChart(mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return spec.ChartName == "hcloud/hcloud-csi" &&
				spec.ReleaseName == "hcloud-csi" &&
				spec.Namespace == "kube-system"
		})).
		Return(mockManifest, nil).
		Once()

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.Len(t, images, 2)
	assert.Contains(t, images, "docker.io/hetznercloud/hcloud-csi-driver:v2.5.0")
	assert.Contains(t, images, "registry.k8s.io/sig-storage/csi-attacher:v4.3.0")
	mockClient.AssertExpectations(t)
}

func TestHetznerCSIInstaller_Images_TemplatingFails(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	mockClient.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", assert.AnError).
		Once()

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	images, err := installer.Images(context.Background())

	require.Error(t, err)
	assert.Nil(t, images)
	assert.Contains(t, err.Error(), "failed to template hetzner-csi chart")
	mockClient.AssertExpectations(t)
}

func TestHetznerCSIInstaller_Images_NoImages(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	// Manifest with no container specs
	emptyManifest := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  key: value
`

	mockClient.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return(emptyManifest, nil).
		Once()

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.Empty(t, images)
	mockClient.AssertExpectations(t)
}

func TestHetznerCSIInstaller_Images_EmptyManifest(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	mockClient.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", nil).
		Once()

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.Empty(t, images)
	mockClient.AssertExpectations(t)
}
