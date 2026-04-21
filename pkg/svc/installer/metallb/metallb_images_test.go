package metallbinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	metallbinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metallb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInstaller_Images_EmptyManifest(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", nil)

	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"",
	)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.Empty(t, images)
}

func TestInstaller_Install_FailsOnPrivilegedNamespace(t *testing.T) {
	t.Parallel()

	// Install calls ensurePrivilegedNamespace which calls k8s.NewClientset
	// with an invalid kubeconfig path, so it should fail.
	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"/nonexistent/kubeconfig",
		"test-context",
		5*time.Minute,
		"",
	)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to ensure privileged namespace")
}
