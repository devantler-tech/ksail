package cloudproviderkindinstaller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCloudProviderKINDInstaller(t *testing.T) {
	t.Parallel()

	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller()

	assert.NotNil(t, installer)
}

func TestCloudProviderKINDInstallerInstall(t *testing.T) {
	// Note: This is an integration test that actually starts the controller
	// Skip in CI environments where Docker might not be available
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping integration test in CI")
	}

	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller()

	ctx := context.Background()
	err := installer.Install(ctx)

	// Clean up
	defer func() {
		_ = installer.Uninstall(ctx)
	}()

	require.NoError(t, err)
}

func TestCloudProviderKINDInstallerInstallMultipleCalls(t *testing.T) {
	// Skip in CI
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping integration test in CI")
	}

	installer1 := cloudproviderkindinstaller.NewCloudProviderKINDInstaller()
	installer2 := cloudproviderkindinstaller.NewCloudProviderKINDInstaller()

	ctx := context.Background()

	// First install
	err := installer1.Install(ctx)
	require.NoError(t, err)

	// Second install (should increment reference count)
	err = installer2.Install(ctx)
	require.NoError(t, err)

	// Uninstall first (should not stop controller)
	err = installer1.Uninstall(ctx)
	require.NoError(t, err)

	// Uninstall second (should stop controller)
	err = installer2.Uninstall(ctx)
	require.NoError(t, err)
}

func TestCloudProviderKINDInstallerUninstallNoInstall(t *testing.T) {
	t.Parallel()

	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller()

	ctx := context.Background()
	err := installer.Uninstall(ctx)

	require.NoError(t, err)
}

func TestLockFileOperations(t *testing.T) {
	t.Parallel()

	// Get the lock file path
	tmpDir := os.TempDir()
	lockPath := filepath.Join(tmpDir, "cloud-provider-kind.lock")

	// Clean up any existing lock file
	_ = os.Remove(lockPath)

	// Verify lock file doesn't exist initially
	_, err := os.Stat(lockPath)
	assert.True(t, os.IsNotExist(err))

	// Note: We can't directly test the lock file functions since they're not exported
	// They are tested indirectly through Install/Uninstall tests
}

