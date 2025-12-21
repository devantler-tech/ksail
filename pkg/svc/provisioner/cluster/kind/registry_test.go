package kindprovisioner_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	docker "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/errdefs"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var (
	errContainerListFailed  = errors.New("list failed")
	errRegistryCreateFailed = errors.New("registry create failed")
	errRegistryNotFound     = errors.New("not found")
	errNetworkNotFound      = errors.New("network not found")
)

func TestMain(m *testing.M) {
	v := m.Run()

	// After all tests have run, clean up snapshots
	_, _ = snaps.Clean(m)

	os.Exit(v)
}

// loadTestData loads test data from testdata directory.
func loadTestData(t *testing.T, filename string) string {
	t.Helper()
	//nolint:gosec // Test data files are safe
	data, err := os.ReadFile(filepath.Join("testdata", filename))
	if err != nil {
		t.Fatalf("failed to load test data %s: %v", filename, err)
	}

	return string(data)
}

// setupTestEnvironment creates a standard test environment with mock client, context, and buffer.
func setupTestEnvironment(t *testing.T) (*docker.MockAPIClient, context.Context, *bytes.Buffer) {
	t.Helper()
	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()
	buf := &bytes.Buffer{}

	return mockClient, ctx, buf
}

func expectRegistryPortScan(
	mockClient *docker.MockAPIClient,
	registries []container.Summary,
) {
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return(registries, nil).
		Once()
}

func matchListOptionsByName(name string) any {
	return mock.MatchedBy(func(opts container.ListOptions) bool {
		values := opts.Filters.Get("name")
		if len(values) == 0 {
			return false
		}

		return slices.Contains(values, name)
	})
}

func TestSetupRegistries_NilKindConfig(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	var buf bytes.Buffer

	err := kindprovisioner.SetupRegistries(ctx, nil, "test-cluster", mockClient, nil, &buf)
	assert.NoError(t, err)
}

func TestSetupRegistries_NoRegistries(t *testing.T) {
	t.Parallel()

	mockClient, ctx, buf := setupTestEnvironment(t)

	kindConfig := &v1alpha4.Cluster{
		ContainerdConfigPatches: []string{},
	}

	err := kindprovisioner.SetupRegistries(ctx, kindConfig, "test-cluster", mockClient, nil, buf)
	assert.NoError(t, err)
}

func TestSetupRegistries_NilDockerClient(t *testing.T) {
	t.Parallel()

	patch := `[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
  endpoint = ["http://localhost:5000"]`

	kindConfig := &v1alpha4.Cluster{
		ContainerdConfigPatches: []string{patch},
	}

	err := kindprovisioner.SetupRegistries(
		context.Background(),
		kindConfig,
		"test",
		nil,
		nil,
		io.Discard,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create registry manager")
}

func TestSetupRegistries_CreateRegistryError(t *testing.T) {
	t.Parallel()

	mockClient, ctx, buf := setupTestEnvironment(t)

	patch := `[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
  endpoint = ["http://localhost:5000"]`

	kindConfig := &v1alpha4.Cluster{
		ContainerdConfigPatches: []string{patch},
	}

	expectRegistryPortScan(mockClient, []container.Summary{})
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()
	mockClient.EXPECT().ContainerList(ctx, mock.Anything).Return(nil, errContainerListFailed).Once()

	err := kindprovisioner.SetupRegistries(ctx, kindConfig, "test", mockClient, nil, buf)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create registry")
}

func TestSetupRegistries_CleansUpAfterPartialFailure(t *testing.T) {
	t.Parallel()

	runSetupRegistriesPartialFailureScenario(t)
}

func TestSetupRegistries_DoesNotRemoveExistingRegistriesOnFailure(t *testing.T) {
	t.Parallel()

	runSetupRegistriesExistingRegistryScenario(t)
}

func runSetupRegistriesPartialFailureScenario(t *testing.T) {
	t.Helper()

	mockClient, ctx, buf := setupTestEnvironment(t)
	kindConfig := newTwoMirrorKindConfig()
	firstRegistryID := "docker.io-id"

	expectInitialRegistryScan(mockClient)
	expectMirrorProvisionSuccess(mockClient, "docker.io", firstRegistryID)
	expectMirrorProvisionFailure(mockClient, "ghcr.io", errRegistryCreateFailed)
	expectCleanupRunningRegistry(mockClient, firstRegistryID, "docker.io")

	err := kindprovisioner.SetupRegistries(ctx, kindConfig, "test", mockClient, nil, buf)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create registry ghcr.io")
	mockClient.AssertExpectations(t)
}

func runSetupRegistriesExistingRegistryScenario(t *testing.T) {
	t.Helper()

	mockClient, ctx, buf := setupTestEnvironment(t)
	kindConfig := newTwoMirrorKindConfig()

	existing := container.Summary{
		ID:    "docker.io-id",
		State: "running",
		Names: []string{"/docker.io"},
		Labels: map[string]string{
			docker.RegistryLabelKey: "docker.io",
		},
	}

	// Existing registry is discovered before provisioning new mirrors.
	expectRegistryPortScan(mockClient, []container.Summary{existing})
	mockClient.EXPECT().
		ContainerList(mock.Anything, matchListOptionsByName("docker.io")).
		Return([]container.Summary{existing}, nil).
		Once()
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{existing}, nil).
		Once()
	mockClient.EXPECT().
		ContainerList(mock.Anything, matchListOptionsByName("docker.io")).
		Return([]container.Summary{existing}, nil).
		Once()

	expectMirrorProvisionFailure(mockClient, "ghcr.io", errRegistryCreateFailed)

	err := kindprovisioner.SetupRegistries(ctx, kindConfig, "test", mockClient, nil, buf)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create registry ghcr.io")

	mockClient.AssertNotCalled(t, "ContainerStop", mock.Anything, mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "ContainerRemove", mock.Anything, mock.Anything, mock.Anything)
	mockClient.AssertExpectations(t)
}

func newTwoMirrorKindConfig() *v1alpha4.Cluster {
	patch := `[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
  endpoint = ["http://localhost:5000"]
[plugins."io.containerd.grpc.v1.cri".registry.mirrors."ghcr.io"]
  endpoint = ["http://localhost:5001"]`

	return &v1alpha4.Cluster{ContainerdConfigPatches: []string{patch}}
}

func expectInitialRegistryScan(mockClient *docker.MockAPIClient) {
	expectRegistryPortScan(mockClient, []container.Summary{})
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()
}

func expectMirrorProvisionBase(
	mockClient *docker.MockAPIClient,
	sanitized string,
) {
	mockClient.EXPECT().
		ContainerList(mock.Anything, matchListOptionsByName(sanitized)).
		Return([]container.Summary{}, nil).
		Once()
	mockClient.EXPECT().
		ImageInspect(mock.Anything, docker.RegistryImageName).
		Return(image.InspectResponse{}, nil).
		Once()
	mockClient.EXPECT().
		VolumeInspect(mock.Anything, sanitized).
		Return(volume.Volume{}, errRegistryNotFound).
		Once()
	mockClient.EXPECT().
		VolumeCreate(mock.Anything, mock.Anything).
		Return(volume.Volume{}, nil).
		Once()
}

func expectMirrorProvisionSuccess(
	mockClient *docker.MockAPIClient,
	sanitized string,
	containerID string,
) {
	expectMirrorProvisionBase(mockClient, sanitized)

	expectMirrorContainerCreate(
		mockClient,
		sanitized,
		container.CreateResponse{ID: containerID},
		nil,
	)
	mockClient.EXPECT().
		ContainerStart(mock.Anything, containerID, mock.Anything).
		Return(nil).
		Once()
}

func expectMirrorProvisionFailure(
	mockClient *docker.MockAPIClient,
	sanitized string,
	createErr error,
) {
	expectMirrorProvisionBase(mockClient, sanitized)

	expectMirrorContainerCreate(
		mockClient,
		sanitized,
		container.CreateResponse{},
		createErr,
	)
}

func expectMirrorContainerCreate(
	mockClient *docker.MockAPIClient,
	sanitized string,
	response container.CreateResponse,
	returnErr error,
) {
	containerName := sanitized
	mockClient.EXPECT().
		ContainerCreate(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, containerName).
		Return(response, returnErr).
		Once()
}

func expectCleanupRunningRegistry(
	mockClient *docker.MockAPIClient,
	containerID string,
	name string,
) {
	mockClient.EXPECT().ContainerList(mock.Anything, mock.Anything).Return([]container.Summary{
		{
			ID:    containerID,
			State: "running",
			Names: []string{"/" + name},
			Labels: map[string]string{
				docker.RegistryLabelKey: name,
			},
		},
	}, nil).Once()
	mockClient.EXPECT().
		ContainerInspect(mock.Anything, containerID).
		Return(newInspectResponse(), nil).
		Once()
	mockClient.EXPECT().
		NetworkDisconnect(mock.Anything, "kind", containerID, true).
		Return(errdefs.NotFound(errNetworkNotFound)).
		Once()
	mockClient.EXPECT().
		ContainerInspect(mock.Anything, containerID).
		Return(newInspectResponse(), nil).
		Once()
	mockClient.EXPECT().
		ContainerStop(mock.Anything, containerID, mock.Anything).
		Return(nil).
		Once()
	mockClient.EXPECT().
		ContainerRemove(mock.Anything, containerID, mock.Anything).
		Return(nil).
		Once()
}

func newInspectResponse(networks ...string) container.InspectResponse {
	sanitized := make(map[string]*network.EndpointSettings, len(networks))
	for _, name := range networks {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}

		sanitized[trimmed] = &network.EndpointSettings{}
	}

	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{},
		NetworkSettings: &container.NetworkSettings{
			Networks: sanitized,
		},
	}
}

func TestConnectRegistriesToNetwork_NilMirrorSpecs(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	var buf bytes.Buffer

	err := kindprovisioner.ConnectRegistriesToNetwork(ctx, nil, mockClient, &buf)
	assert.NoError(t, err)
}

func TestConnectRegistriesToNetwork_NoRegistries(t *testing.T) {
	t.Parallel()

	mockClient, ctx, buf := setupTestEnvironment(t)

	emptySpecs := []registry.MirrorSpec{}

	err := kindprovisioner.ConnectRegistriesToNetwork(ctx, emptySpecs, mockClient, buf)
	assert.NoError(t, err)
}

func TestCleanupRegistries_NilMirrorSpecs(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	err := kindprovisioner.CleanupRegistries(ctx, nil, "test-cluster", mockClient, false)
	assert.NoError(t, err)
}

func TestCleanupRegistries_NoRegistries(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	emptySpecs := []registry.MirrorSpec{}

	err := kindprovisioner.CleanupRegistries(ctx, emptySpecs, "test-cluster", mockClient, false)
	assert.NoError(t, err)
}

// Deprecated test removed - uses legacy ExtractRegistriesFromKindForTesting
