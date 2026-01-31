package cloudproviderkindinstaller_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockDockerClient is a mock for the Docker API client.
type MockDockerClient struct {
	mock.Mock
}

func (m *MockDockerClient) ContainerList(
	ctx context.Context,
	opts container.ListOptions,
) ([]container.Summary, error) {
	args := m.Called(ctx, opts)

	return args.Get(0).([]container.Summary), args.Error(1)
}

func (m *MockDockerClient) ImageInspect(
	ctx context.Context,
	imageName string,
) (image.InspectResponse, error) {
	args := m.Called(ctx, imageName)

	return args.Get(0).(image.InspectResponse), args.Error(1)
}

func (m *MockDockerClient) ImagePull(
	ctx context.Context,
	imageName string,
	opts image.PullOptions,
) (io.ReadCloser, error) {
	args := m.Called(ctx, imageName, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockDockerClient) ContainerCreate(
	ctx context.Context,
	config *container.Config,
	hostConfig *container.HostConfig,
	networkingConfig interface{},
	platform interface{},
	containerName string,
) (container.CreateResponse, error) {
	args := m.Called(ctx, config, hostConfig, networkingConfig, platform, containerName)

	return args.Get(0).(container.CreateResponse), args.Error(1)
}

func (m *MockDockerClient) ContainerStart(
	ctx context.Context,
	containerID string,
	opts container.StartOptions,
) error {
	args := m.Called(ctx, containerID, opts)

	return args.Error(0)
}

func (m *MockDockerClient) ContainerStop(
	ctx context.Context,
	containerID string,
	opts container.StopOptions,
) error {
	args := m.Called(ctx, containerID, opts)

	return args.Error(0)
}

func (m *MockDockerClient) ContainerRemove(
	ctx context.Context,
	containerID string,
	opts container.RemoveOptions,
) error {
	args := m.Called(ctx, containerID, opts)

	return args.Error(0)
}

func TestNewCloudProviderKINDInstaller(t *testing.T) {
	t.Parallel()

	client := new(MockDockerClient)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(client)

	assert.NotNil(t, installer)
}

func TestCloudProviderKINDInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	client := new(MockDockerClient)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(client)

	// Container not running
	client.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	// Image exists
	client.On("ImageInspect", mock.Anything, cloudproviderkindinstaller.CloudProviderKindImage).
		Return(image.InspectResponse{}, nil).
		Once()

	// Container create
	client.On("ContainerCreate",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		cloudproviderkindinstaller.CloudProviderKindContainerName,
	).Return(container.CreateResponse{ID: "test-id"}, nil).
		Once()

	// Container start
	client.On("ContainerStart", mock.Anything, "test-id", mock.Anything).
		Return(nil).
		Once()

	err := installer.Install(context.Background())

	require.NoError(t, err)
	client.AssertExpectations(t)
}

func TestCloudProviderKINDInstallerInstallAlreadyRunning(t *testing.T) {
	t.Parallel()

	client := new(MockDockerClient)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(client)

	// Container already running
	client.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{
			{
				ID:    "test-id",
				State: "running",
			},
		}, nil).
		Once()

	err := installer.Install(context.Background())

	require.NoError(t, err)
	client.AssertExpectations(t)
}

func TestCloudProviderKINDInstallerInstallWithImagePull(t *testing.T) {
	t.Parallel()

	client := new(MockDockerClient)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(client)

	// Container not running
	client.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	// Image doesn't exist
	client.On("ImageInspect", mock.Anything, cloudproviderkindinstaller.CloudProviderKindImage).
		Return(image.InspectResponse{}, assert.AnError).
		Once()

	// Pull image
	reader := io.NopCloser(strings.NewReader(""))
	client.On("ImagePull",
		mock.Anything,
		cloudproviderkindinstaller.CloudProviderKindImage,
		mock.Anything,
	).Return(reader, nil).
		Once()

	// Container create
	client.On("ContainerCreate",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		cloudproviderkindinstaller.CloudProviderKindContainerName,
	).Return(container.CreateResponse{ID: "test-id"}, nil).
		Once()

	// Container start
	client.On("ContainerStart", mock.Anything, "test-id", mock.Anything).
		Return(nil).
		Once()

	err := installer.Install(context.Background())

	require.NoError(t, err)
	client.AssertExpectations(t)
}

func TestCloudProviderKINDInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	client := new(MockDockerClient)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(client)

	// Container exists and is running
	client.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{
			{
				ID:    "test-id",
				State: "running",
			},
		}, nil).
		Once()

	// Stop container
	client.On("ContainerStop", mock.Anything, "test-id", mock.Anything).
		Return(nil).
		Once()

	// Remove container
	client.On("ContainerRemove", mock.Anything, "test-id", mock.Anything).
		Return(nil).
		Once()

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
	client.AssertExpectations(t)
}

func TestCloudProviderKINDInstallerUninstallNoContainer(t *testing.T) {
	t.Parallel()

	client := new(MockDockerClient)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(client)

	// No container exists
	client.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
	client.AssertExpectations(t)
}

