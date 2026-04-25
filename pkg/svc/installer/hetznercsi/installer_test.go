package hetznercsiinstaller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hetznercsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errSimulatedCCMLabelTimeout = errors.New("simulated CCM label timeout")

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		networkName string
	}{
		{name: "without network name", networkName: ""},
		{name: "with network name", networkName: "dev-network"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient := helm.NewMockInterface(t)
			timeout := 5 * time.Minute

			installer := hetznercsiinstaller.NewInstaller(
				mockClient,
				"~/.kube/config",
				"test-context",
				timeout,
				testCase.networkName,
			)

			assert.NotNil(t, installer)
		})
	}
}

func TestBuildSecretData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		networkName string
		wantNil     bool
		wantValue   string
	}{
		{name: "empty network name returns nil", networkName: "", wantNil: true},
		{
			name:        "network name stored in secret data",
			networkName: "dev-network",
			wantValue:   "dev-network",
		},
		{
			name:        "custom network name stored in secret data",
			networkName: "my-custom-net",
			wantValue:   "my-custom-net",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hetznercsiinstaller.BuildSecretDataForTest(testCase.networkName)

			if testCase.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, testCase.wantValue, string(result["network"]))
			}
		})
	}
}

func TestHetznerCSIInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	// Mock UninstallRelease call
	mockClient.EXPECT().
		UninstallRelease(mock.Anything, "hcloud-csi", "kube-system").
		Return(nil).
		Once()

	installer := hetznercsiinstaller.NewInstaller(
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

// TestHetznerCSIInstaller_Install_WaitsForCCMLabels verifies that Install
// invokes the CCM node-label wait with the installer's kubeconfig, context,
// and timeout before any Helm work, and that it aborts when the wait fails —
// preventing the csi-node DaemonSet from starting before nodes are labeled.
//
//nolint:paralleltest // Mutates package-level wait seam via SetWaitForCCMNodeLabelsFnForTest.
func TestHetznerCSIInstaller_Install_WaitsForCCMLabels(t *testing.T) {
	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	waitErr := errSimulatedCCMLabelTimeout

	var (
		gotKubeconfig, gotContext string
		gotDeadline               time.Duration
		called                    bool
	)

	restore := hetznercsiinstaller.SetWaitForCCMNodeLabelsFnForTest(
		func(_ context.Context, kubeconfig, kubeContext string, deadline time.Duration) error {
			called = true
			gotKubeconfig = kubeconfig
			gotContext = kubeContext
			gotDeadline = deadline

			return waitErr
		},
	)
	defer restore()

	installer := hetznercsiinstaller.NewInstaller(
		mockClient,
		"/tmp/kubeconfig",
		"test-context",
		timeout,
	)

	err := installer.Install(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, waitErr)
	assert.True(t, called, "CCM label wait must be invoked before Install")
	assert.Equal(t, "/tmp/kubeconfig", gotKubeconfig)
	assert.Equal(t, "test-context", gotContext)
	assert.Equal(t, timeout, gotDeadline)

	// Helm must not be touched when the wait fails.
	mockClient.AssertExpectations(t)
}
