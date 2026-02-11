package cloudproviderkindinstaller_test

import (
	"context"
	"os"
	"testing"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ciEnvValue = "true"

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	// Skip in CI - requires Docker
	if os.Getenv("CI") == ciEnvValue {
		t.Skip("Skipping test that requires Docker in CI")
	}

	dockerClient, err := dockerclient.GetDockerClient()
	require.NoError(t, err)

	defer func() { _ = dockerClient.Close() }()

	installer := cloudproviderkindinstaller.NewInstaller(dockerClient)

	assert.NotNil(t, installer)
}

func TestCloudProviderKINDInstallerInstallAndUninstall(t *testing.T) {
	t.Parallel()

	// Note: This is an integration test that actually starts the controller
	// Skip in CI environments where Docker might not be available
	if os.Getenv("CI") == ciEnvValue {
		t.Skip("Skipping integration test in CI")
	}

	dockerClient, err := dockerclient.GetDockerClient()
	require.NoError(t, err)

	defer func() { _ = dockerClient.Close() }()

	installer := cloudproviderkindinstaller.NewInstaller(dockerClient)

	ctx := context.Background()

	// Install - this creates and starts the container
	err = installer.Install(ctx)
	require.NoError(t, err)

	// Clean up
	err = installer.Uninstall(ctx)
	require.NoError(t, err)
}

func TestCloudProviderKINDInstallerUninstallNoInstall(t *testing.T) {
	t.Parallel()

	// Skip in CI - requires Docker
	if os.Getenv("CI") == ciEnvValue {
		t.Skip("Skipping test that requires Docker in CI")
	}

	dockerClient, err := dockerclient.GetDockerClient()
	require.NoError(t, err)

	defer func() { _ = dockerClient.Close() }()

	installer := cloudproviderkindinstaller.NewInstaller(dockerClient)

	ctx := context.Background()
	err = installer.Uninstall(ctx)

	// Uninstall when nothing is installed should succeed (no-op)
	require.NoError(t, err)
}

func TestCloudProviderKindImage(t *testing.T) {
	t.Parallel()

	image := cloudproviderkindinstaller.CloudProviderKindImage()

	// Verify the image is parsed from the Dockerfile
	assert.NotEmpty(t, image)
	assert.Contains(t, image, "registry.k8s.io/cloud-provider-kind/cloud-controller-manager")
}
