package hetznercsiinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hetznercsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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
		{name: "network name stored in secret data", networkName: "dev-network", wantValue: "dev-network"},
		{name: "custom network name stored in secret data", networkName: "my-custom-net", wantValue: "my-custom-net"},
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
