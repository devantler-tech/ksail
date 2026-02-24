package cloudproviderkindinstaller_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const ciEnvValue = "true"

var (
	errNotFound             = errors.New("not found")
	errDockerDaemonError    = errors.New("docker daemon error")
	errStartFailed          = errors.New("start failed")
	errListError            = errors.New("list error")
	errNetworkAlreadyExists = errors.New("network already exists")
)

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

// Unit tests using mocks (safe to run in CI)

func TestInstall_ContainerAlreadyRunning(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: container exists and is running
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{
			{
				ID:    "test-container-id",
				Names: []string{"/" + cloudproviderkindinstaller.ContainerName},
				State: "running",
			},
		}, nil).
		Once()

	err := installer.Install(ctx)
	require.NoError(t, err)
}

func TestInstall_ContainerExistsButStopped(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// First call: check if running (returns stopped container)
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{
			{
				ID:    "stopped-container-id",
				Names: []string{"/" + cloudproviderkindinstaller.ContainerName},
				State: "exited",
			},
		}, nil).
		Once()

	// Second call: check if exists (returns stopped container)
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{
			{
				ID:    "stopped-container-id",
				Names: []string{"/" + cloudproviderkindinstaller.ContainerName},
				State: "exited",
			},
		}, nil).
		Once()

	// Mock: start the stopped container
	mockClient.EXPECT().
		ContainerStart(ctx, cloudproviderkindinstaller.ContainerName, mock.Anything).
		Return(nil).
		Once()

	err := installer.Install(ctx)
	require.NoError(t, err)
}

func TestInstall_CreateNewContainer(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	imageName := cloudproviderkindinstaller.CloudProviderKindImage()

	// First call: check if running (no containers)
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{}, nil).
		Once()

	// Second call: check if exists (no containers)
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{}, nil).
		Once()

	// Mock: image exists (skip pull)
	mockClient.EXPECT().
		ImageInspect(ctx, imageName).
		Return(image.InspectResponse{}, nil).
		Once()

	// Mock: network exists (skip create)
	mockClient.EXPECT().
		NetworkInspect(ctx, cloudproviderkindinstaller.KindNetworkName, mock.Anything).
		Return(network.Inspect{}, nil).
		Once()

	// Mock: create container
	cname := cloudproviderkindinstaller.ContainerName

	mockClient.EXPECT().
		ContainerCreate(ctx, mock.Anything, mock.Anything, mock.Anything, (*v1.Platform)(nil), cname).
		Return(container.CreateResponse{ID: "new-container-id"}, nil).
		Once()

	// Mock: start container
	mockClient.EXPECT().
		ContainerStart(ctx, "new-container-id", mock.Anything).
		Return(nil).
		Once()

	err := installer.Install(ctx)
	require.NoError(t, err)
}

func TestInstall_CreateWithImagePull(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	imageName := cloudproviderkindinstaller.CloudProviderKindImage()

	// First call: check if running (no containers)
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{}, nil).
		Once()

	// Second call: check if exists (no containers)
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{}, nil).
		Once()

	// Mock: image doesn't exist (need to pull)
	mockClient.EXPECT().
		ImageInspect(ctx, imageName).
		Return(image.InspectResponse{}, errNotFound).
		Once()

	// Mock: pull image
	pullReader := io.NopCloser(strings.NewReader("pulling..."))
	mockClient.EXPECT().
		ImagePull(ctx, imageName, mock.Anything).
		Return(pullReader, nil).
		Once()

	// Mock: network exists
	mockClient.EXPECT().
		NetworkInspect(ctx, cloudproviderkindinstaller.KindNetworkName, mock.Anything).
		Return(network.Inspect{}, nil).
		Once()

	// Mock: create and start container
	cname := cloudproviderkindinstaller.ContainerName

	mockClient.EXPECT().
		ContainerCreate(ctx, mock.Anything, mock.Anything, mock.Anything, (*v1.Platform)(nil), cname).
		Return(container.CreateResponse{ID: "new-id"}, nil).
		Once()

	mockClient.EXPECT().
		ContainerStart(ctx, "new-id", mock.Anything).
		Return(nil).
		Once()

	err := installer.Install(ctx)
	require.NoError(t, err)
}

func TestInstall_CreateWithNetworkCreation(t *testing.T) { //nolint:dupl
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	imageName := cloudproviderkindinstaller.CloudProviderKindImage()

	// Mock: no running containers
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Times(2)

	// Mock: image exists
	mockClient.EXPECT().
		ImageInspect(ctx, imageName).
		Return(image.InspectResponse{}, nil).
		Once()

	// Mock: network doesn't exist (need to create)
	mockClient.EXPECT().
		NetworkInspect(ctx, cloudproviderkindinstaller.KindNetworkName, mock.Anything).
		Return(network.Inspect{}, errNotFound).
		Once()

	// Mock: create network
	mockClient.EXPECT().
		NetworkCreate(ctx, cloudproviderkindinstaller.KindNetworkName, mock.Anything).
		Return(network.CreateResponse{}, nil).
		Once()

	// Mock: create and start container
	cname := cloudproviderkindinstaller.ContainerName

	mockClient.EXPECT().
		ContainerCreate(ctx, mock.Anything, mock.Anything, mock.Anything, (*v1.Platform)(nil), cname).
		Return(container.CreateResponse{ID: "new-id"}, nil).
		Once()

	mockClient.EXPECT().
		ContainerStart(ctx, "new-id", mock.Anything).
		Return(nil).
		Once()

	err := installer.Install(ctx)
	require.NoError(t, err)
}

func TestInstall_ErrorCheckingIfRunning(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: error listing containers
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(nil, errDockerDaemonError).
		Once()

	err := installer.Install(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check container status")
}

func TestInstall_ErrorStartingExisting(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: container exists but stopped
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{{State: "exited"}}, nil).
		Times(2)

	// Mock: error starting container
	mockClient.EXPECT().
		ContainerStart(ctx, cloudproviderkindinstaller.ContainerName, mock.Anything).
		Return(errStartFailed).
		Once()

	err := installer.Install(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start existing container")
}

func TestUninstall_RemovesRunningContainer(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: container exists and is running
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:    "running-id",
				Names: []string{"/" + cloudproviderkindinstaller.ContainerName},
				State: "running",
			},
		}, nil).
		Once()

	// Mock: stop container
	mockClient.EXPECT().
		ContainerStop(ctx, "running-id", mock.Anything).
		Return(nil).
		Once()

	// Mock: remove container
	mockClient.EXPECT().
		ContainerRemove(ctx, "running-id", mock.Anything).
		Return(nil).
		Once()

	// Mock: list cpk- containers (none found)
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{}, nil).
		Once()

	err := installer.Uninstall(ctx)
	require.NoError(t, err)
}

func TestUninstall_RemovesStoppedContainer(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: container exists but is stopped
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:    "stopped-id",
				Names: []string{"/" + cloudproviderkindinstaller.ContainerName},
				State: "exited",
			},
		}, nil).
		Once()

	// Mock: remove container (no need to stop)
	mockClient.EXPECT().
		ContainerRemove(ctx, "stopped-id", mock.Anything).
		Return(nil).
		Once()

	// Mock: list cpk- containers (none found)
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	err := installer.Uninstall(ctx)
	require.NoError(t, err)
}

func TestUninstall_NoContainerExists(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: no containers found
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Times(2) // Once for main container, once for cpk- containers

	err := installer.Uninstall(ctx)
	require.NoError(t, err)
}

func TestUninstall_CleansCPKContainers(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: main container doesn't exist
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	// Mock: cpk- containers exist
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All
		})).
		Return([]container.Summary{
			{ID: "cpk-lb-1", Names: []string{"/cpk-lb-test"}, State: "running"},
			{ID: "cpk-lb-2", Names: []string{"/cpk-lb-test2"}, State: "exited"},
		}, nil).
		Once()

	// Mock: stop and remove cpk containers
	mockClient.EXPECT().
		ContainerStop(ctx, "cpk-lb-1", mock.Anything).
		Return(nil).
		Once()

	mockClient.EXPECT().
		ContainerRemove(ctx, "cpk-lb-1", mock.Anything).
		Return(nil).
		Once()

	mockClient.EXPECT().
		ContainerRemove(ctx, "cpk-lb-2", mock.Anything).
		Return(nil).
		Once()

	err := installer.Uninstall(ctx)
	require.NoError(t, err)
}

func TestUninstall_ErrorListingCPKContainers(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	// Mock: main container doesn't exist
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	// Mock: error listing cpk- containers
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(nil, errListError).
		Once()

	err := installer.Uninstall(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cleanup cpk containers")
}

func TestImages_ReturnsCloudProviderKindImage(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	images, err := installer.Images(ctx)
	require.NoError(t, err)
	require.Len(t, images, 1)
	assert.Equal(t, cloudproviderkindinstaller.CloudProviderKindImage(), images[0])
}

func TestInstall_NetworkAlreadyExists(t *testing.T) { //nolint:dupl
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)
	installer := cloudproviderkindinstaller.NewInstaller(mockClient)
	ctx := context.Background()

	imageName := cloudproviderkindinstaller.CloudProviderKindImage()

	// Mock: no containers
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Times(2)

	// Mock: image exists
	mockClient.EXPECT().
		ImageInspect(ctx, imageName).
		Return(image.InspectResponse{}, nil).
		Once()

	// Mock: network creation returns "already exists" error
	mockClient.EXPECT().
		NetworkInspect(ctx, cloudproviderkindinstaller.KindNetworkName, mock.Anything).
		Return(network.Inspect{}, errNotFound).
		Once()

	mockClient.EXPECT().
		NetworkCreate(ctx, cloudproviderkindinstaller.KindNetworkName, mock.Anything).
		Return(network.CreateResponse{}, errNetworkAlreadyExists).
		Once()

	// Mock: create and start container should still proceed
	cname := cloudproviderkindinstaller.ContainerName

	mockClient.EXPECT().
		ContainerCreate(ctx, mock.Anything, mock.Anything, mock.Anything, (*v1.Platform)(nil), cname).
		Return(container.CreateResponse{ID: "new-id"}, nil).
		Once()

	mockClient.EXPECT().
		ContainerStart(ctx, "new-id", mock.Anything).
		Return(nil).
		Once()

	err := installer.Install(ctx)
	require.NoError(t, err)
}
