package kindprovisioner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

func TestExtractRegistriesFromKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inputFile string
		isEmpty   bool
	}{
		{name: "single registry", inputFile: "containerd_single_endpoint.toml"},
		{name: "multiple registries", inputFile: "containerd_multiple_mirrors.toml"},
		{name: "multiple registries same port", inputFile: "containerd_multiple_same_port.toml"},
		{
			name:      "duplicate registries in multiple patches",
			inputFile: "containerd_duplicate_patches.toml",
		},
		{
			name:      "registry with special characters",
			inputFile: "containerd_registry_special_chars.toml",
		},
		{name: "no containerd patches", isEmpty: true},
		{
			name:      "multiple endpoints uses first",
			inputFile: "containerd_multiple_endpoints_inline.toml",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var config *v1alpha4.Cluster
			if testCase.isEmpty {
				config = &v1alpha4.Cluster{ContainerdConfigPatches: []string{}}
			} else {
				patch := loadTestData(t, testCase.inputFile)
				config = &v1alpha4.Cluster{ContainerdConfigPatches: []string{patch}}
			}

			result := kindprovisioner.ExtractRegistriesFromKindForTesting(config, nil)
			snaps.MatchSnapshot(t, result)
		})
	}
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

func TestConnectRegistriesToNetwork_NilKindConfig(t *testing.T) {
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

	kindConfig := &v1alpha4.Cluster{
		ContainerdConfigPatches: []string{},
	}

	err := kindprovisioner.ConnectRegistriesToNetwork(ctx, kindConfig, mockClient, buf)
	assert.NoError(t, err)
}

func TestCleanupRegistries_NilKindConfig(t *testing.T) {
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

	kindConfig := &v1alpha4.Cluster{
		ContainerdConfigPatches: []string{},
	}

	err := kindprovisioner.CleanupRegistries(ctx, kindConfig, "test-cluster", mockClient, false)
	assert.NoError(t, err)
}

func TestBuildRegistryInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		host             string
		endpoints        []string
		expectedName     string
		expectedPort     int
		expectedUpstream string
		expectedVolume   string
	}{
		{
			name:             "docker.io with port in endpoint",
			host:             "docker.io",
			endpoints:        []string{"http://localhost:5000"},
			expectedName:     "docker.io",
			expectedPort:     5000,
			expectedUpstream: "https://registry-1.docker.io",
			expectedVolume:   "docker.io",
		},
		{
			name:             "ghcr.io with port offset",
			host:             "ghcr.io",
			endpoints:        []string{"http://localhost:5001"}, // Provide valid endpoint
			expectedName:     "ghcr.io",
			expectedPort:     5001,
			expectedUpstream: "https://ghcr.io",
			expectedVolume:   "ghcr.io",
		},
		{
			name:             "quay.io with endpoint name extraction",
			host:             "quay.io",
			endpoints:        []string{"http://quay.io:5002"},
			expectedName:     "quay.io",
			expectedPort:     5002,
			expectedUpstream: "https://quay.io",
			expectedVolume:   "quay.io",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Since buildRegistryInfo is unexported, we test it through extractRegistriesFromKind
			patch := fmt.Sprintf(`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."%s"]
  endpoint = %v`, testCase.host, formatEndpoints(testCase.endpoints))

			config := &v1alpha4.Cluster{
				ContainerdConfigPatches: []string{patch},
			}

			registries := kindprovisioner.ExtractRegistriesFromKindForTesting(config, nil)
			assert.Len(t, registries, 1)
			assert.Equal(t, testCase.expectedName, registries[0].Name)
			assert.Equal(t, testCase.expectedUpstream, registries[0].Upstream)
			assert.Equal(t, testCase.expectedPort, registries[0].Port)
			assert.Equal(t, testCase.expectedVolume, registries[0].Volume)
		})
	}
}

func TestExtractRegistriesFromKind_UsesUpstreamOverride(t *testing.T) {
	t.Parallel()

	patch := `[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
  endpoint = ["http://docker.io:5000"]`

	kindConfig := &v1alpha4.Cluster{
		ContainerdConfigPatches: []string{patch},
	}

	registries := kindprovisioner.ExtractRegistriesFromKindForTesting(
		kindConfig,
		map[string]string{"docker.io": "https://mirror.example.com"},
	)

	require.Len(t, registries, 1)
	assert.Equal(t, "https://mirror.example.com", registries[0].Upstream)
}

func formatEndpoints(endpoints []string) string {
	if len(endpoints) == 0 {
		return "[]"
	}

	quoted := make([]string, len(endpoints))
	for i, ep := range endpoints {
		quoted[i] = fmt.Sprintf(`"%s"`, ep)
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}

func TestGenerateUpstreamURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "docker.io special case",
			host:     "docker.io",
			expected: "https://registry-1.docker.io",
		},
		{
			name:     "ghcr.io standard case",
			host:     "ghcr.io",
			expected: "https://ghcr.io",
		},
		{
			name:     "quay.io standard case",
			host:     "quay.io",
			expected: "https://quay.io",
		},
		{
			name:     "custom registry with port",
			host:     "registry.example.com:5000",
			expected: "https://registry.example.com:5000",
		},
	}

	runRegistryExtractionTestCases(
		t,
		tests,
		func(t *testing.T, expected string, registries []registry.Info) {
			t.Helper()
			assert.Equal(t, expected, registries[0].Upstream)
		},
	)
}

// testRegistryExtraction is a helper for testing registry extraction from Kind config.
func testRegistryExtraction(t *testing.T, host, endpoint string) []registry.Info {
	t.Helper()

	patch := fmt.Sprintf(`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."%s"]
  endpoint = ["%s"]`, host, endpoint)

	config := &v1alpha4.Cluster{
		ContainerdConfigPatches: []string{patch},
	}

	return kindprovisioner.ExtractRegistriesFromKindForTesting(config, nil)
}

// runRegistryExtractionTest is a helper to run a single registry extraction test case.
func runRegistryExtractionTest(t *testing.T, host string) []registry.Info {
	t.Helper()
	registries := testRegistryExtraction(t, host, "http://localhost:5000")
	assert.Len(t, registries, 1)

	return registries
}

// runRegistryExtractionTestCases runs a set of test cases with a custom assertion function.
func runRegistryExtractionTestCases(
	t *testing.T,
	tests []struct {
		name     string
		host     string
		expected string
	},
	assertFunc func(*testing.T, string, []registry.Info),
) {
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			registries := runRegistryExtractionTest(t, testCase.host)
			assertFunc(t, testCase.expected, registries)
		})
	}
}

func TestExtractPortFromEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
		expected int
	}{
		{
			name:     "http with port",
			endpoint: "http://localhost:5000",
			expected: 5000,
		},
		{
			name:     "https with port",
			endpoint: "https://registry:5001",
			expected: 5001,
		},
		{
			name:     "with trailing slash",
			endpoint: "http://localhost:5002/",
			expected: 5002,
		},
		{
			name:     "with path",
			endpoint: "http://localhost:5003/v2",
			expected: 5003,
		},
		{
			name:     "no port",
			endpoint: "http://localhost",
			expected: 0,
		},
		{
			name:     "invalid port",
			endpoint: "http://localhost:invalid",
			expected: 0,
		},
		{
			name:     "port too high",
			endpoint: "http://localhost:99999",
			expected: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registries := testRegistryExtraction(t, "test.io", testCase.endpoint)
			if testCase.expected > 0 {
				assert.Len(t, registries, 1)
				assert.Equal(t, testCase.expected, registries[0].Port)
			}
		})
	}
}

func TestGenerateNameFromHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "simple host",
			host:     "docker.io",
			expected: "docker.io",
		},
		{
			name:     "host with subdomain",
			host:     "registry.example.com",
			expected: "registry.example.com",
		},
		{
			name:     "host with port",
			host:     "registry.io:5000",
			expected: "registry.io-5000",
		},
		{
			name:     "host with slashes",
			host:     "example.com/path",
			expected: "example.com-path",
		},
	}

	runRegistryExtractionTestCases(
		t,
		tests,
		func(t *testing.T, expected string, registries []registry.Info) {
			t.Helper()
			assert.Equal(t, expected, registries[0].Name)
		},
	)
}

func TestExtractNameFromEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		endpoint     string
		expectedName string
	}{
		{
			name:         "standard endpoint with port",
			endpoint:     "http://docker.io:5000",
			expectedName: "test.io",
		},
		{
			name:         "https endpoint",
			endpoint:     "https://ghcr.io:5001",
			expectedName: "test.io",
		},
		{
			name:         "non-kind endpoint",
			endpoint:     "http://localhost:5000",
			expectedName: "test.io",
		},
		{
			name:         "malformed endpoint",
			endpoint:     "invalid",
			expectedName: "test.io",
		},
		{
			name:         "endpoint without port",
			endpoint:     "http://kind-registry",
			expectedName: "test.io",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Test through extractRegistriesFromKind
			patch := fmt.Sprintf(`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."test.io"]
  endpoint = ["%s"]`, testCase.endpoint)

			config := &v1alpha4.Cluster{
				ContainerdConfigPatches: []string{patch},
			}

			infos := kindprovisioner.ExtractRegistriesFromKindForTesting(config, nil)
			assert.Len(t, infos, 1)
			assert.Equal(t, testCase.expectedName, infos[0].Name)
			assert.Equal(t, registry.GenerateUpstreamURL("test.io"), infos[0].Upstream)
		})
	}
}
