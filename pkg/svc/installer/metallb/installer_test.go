package metallbinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	metallbinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metallb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		"",
	)

	assert.NotNil(t, installer)
}

func TestNewInstaller_CustomIPRange(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		"10.0.0.100-10.0.0.200",
	)

	assert.NotNil(t, installer)
}

func TestInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	mockClient.EXPECT().
		UninstallRelease(mock.Anything, "metallb", "metallb-system").
		Return(nil).
		Once()

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		"",
	)
	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}
