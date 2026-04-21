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

func TestNewInstaller_DefaultTimeout(t *testing.T) {
	t.Parallel()

	timeout := 7 * time.Minute
	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		timeout,
		"",
	)

	require.NotNil(t, installer)
}

func TestNewInstaller_NilClient(t *testing.T) {
	t.Parallel()

	installer := metallbinstaller.NewInstaller(
		nil,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"",
	)

	require.NotNil(t, installer, "installer should be created even with nil helm client")
}

func TestInstaller_Images_Success(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"",
	)

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: metallb-controller
spec:
  template:
    spec:
      containers:
        - name: controller
          image: quay.io/metallb/controller:v0.15.3
        - name: speaker
          image: quay.io/metallb/speaker:v0.15.3
`
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return(manifest, nil).
		Once()

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.NotEmpty(t, images)
	assert.Contains(t, images, "quay.io/metallb/controller:v0.15.3")
	assert.Contains(t, images, "quay.io/metallb/speaker:v0.15.3")
}

func TestInstaller_Images_TemplateError(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"",
	)

	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", assert.AnError).
		Once()

	images, err := installer.Images(context.Background())

	require.Error(t, err)
	assert.Nil(t, images)
	assert.Contains(t, err.Error(), "template chart")
}

func TestInstaller_Uninstall_ContextTimeout(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"",
	)

	client.EXPECT().
		UninstallRelease(mock.MatchedBy(func(ctx context.Context) bool {
			return ctx.Err() != nil
		}), "metallb", "metallb-system").
		Return(context.DeadlineExceeded).
		Once()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := installer.Uninstall(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestNewInstaller_DefaultIPRange(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"",
	)

	ipRange := metallbinstaller.ExportIPRange(installer)
	assert.Equal(t, "172.18.255.200-172.18.255.250", ipRange,
		"empty ipRange should default to the Docker network range")
}

func TestNewInstaller_ExplicitIPRange(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"10.0.0.1-10.0.0.50",
	)

	ipRange := metallbinstaller.ExportIPRange(installer)
	assert.Equal(t, "10.0.0.1-10.0.0.50", ipRange)
}
