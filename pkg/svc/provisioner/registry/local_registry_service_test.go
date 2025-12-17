package registry_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail-go/pkg/client/docker"
	registry "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockRegistryBackend struct {
	mock.Mock
}

func (m *mockRegistryBackend) CreateRegistry(
	ctx context.Context,
	cfg dockerclient.RegistryConfig,
) error {
	args := m.Called(ctx, cfg)

	return wrapMockError("CreateRegistry", args.Error(0))
}

func (m *mockRegistryBackend) DeleteRegistry(
	ctx context.Context,
	name, clusterName string,
	deleteVolume bool,
	networkName string,
	volumeName string,
) error {
	args := m.Called(ctx, name, clusterName, deleteVolume, networkName, volumeName)

	return wrapMockError("DeleteRegistry", args.Error(0))
}

func (m *mockRegistryBackend) ListRegistries(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)

	if registries, ok := args.Get(0).([]string); ok {
		return registries, wrapMockError("ListRegistries", args.Error(1))
	}

	return nil, wrapMockError("ListRegistries", args.Error(1))
}

func (m *mockRegistryBackend) GetRegistryPort(ctx context.Context, name string) (int, error) {
	args := m.Called(ctx, name)

	return args.Int(0), wrapMockError("GetRegistryPort", args.Error(1))
}

type registryTestHarness struct {
	docker  *dockerclient.MockAPIClient
	backend *mockRegistryBackend
	svc     registry.Service
}

func newRegistryTestHarness(t *testing.T) registryTestHarness {
	t.Helper()

	docker := dockerclient.NewMockAPIClient(t)
	backend := &mockRegistryBackend{}

	svc, err := registry.NewService(registry.Config{DockerClient: docker, RegistryManager: backend})
	require.NoError(t, err)

	return registryTestHarness{docker: docker, backend: backend, svc: svc}
}

func TestCreateOptionsWithDefaults(t *testing.T) {
	t.Parallel()

	opts := registry.CreateOptions{
		Name: "dev-cluster-registry",
		Port: 5000,
	}

	defaulted := opts.WithDefaults()

	require.Equal(t, registry.DefaultEndpointHost, defaulted.Host)
	require.Equal(t, opts.Name, defaulted.VolumeName)
}

func TestCreateOptionsValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		opts    registry.CreateOptions
		wantErr error
	}{
		{
			name: "valid configuration",
			opts: registry.CreateOptions{Name: "ksail", Port: 5000},
		},
		{
			name:    "missing name",
			opts:    registry.CreateOptions{Port: 5000},
			wantErr: registry.ErrNameRequired,
		},
		{
			name:    "invalid port",
			opts:    registry.CreateOptions{Name: "ksail", Port: 70000},
			wantErr: registry.ErrInvalidPort,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.opts.Validate()
			if testCase.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

func TestCreateOptionsEndpoint(t *testing.T) {
	t.Parallel()

	opts := registry.CreateOptions{Name: "ksail", Port: 5000}
	endpoint := opts.Endpoint()

	require.Equal(t, "localhost:5000", endpoint)
}

func TestStartStopStatusOptionValidation(t *testing.T) {
	t.Parallel()

	require.NoError(t, (registry.StartOptions{Name: "ksail"}).Validate())
	require.ErrorIs(t, (registry.StartOptions{}).Validate(), registry.ErrNameRequired)

	require.NoError(t, (registry.StopOptions{Name: "ksail"}).Validate())
	require.ErrorIs(t, (registry.StopOptions{}).Validate(), registry.ErrNameRequired)

	require.NoError(t, (registry.StatusOptions{Name: "ksail"}).Validate())
	require.ErrorIs(t, (registry.StatusOptions{}).Validate(), registry.ErrNameRequired)
}

func TestNewServiceRequiresDockerClient(t *testing.T) {
	t.Parallel()

	_, err := registry.NewService(registry.Config{})
	require.Error(t, err)
}

func TestCreateEnsuresRegistryMetadata(t *testing.T) {
	t.Parallel()

	harness := newRegistryTestHarness(t)

	harness.backend.
		On("ListRegistries", mock.Anything).
		Return([]string{}, nil).
		Once()

	harness.backend.
		On(
			"CreateRegistry",
			mock.Anything,
			mock.MatchedBy(func(cfg dockerclient.RegistryConfig) bool {
				return cfg.Name == "local-registry" && cfg.Port == 5000 &&
					cfg.VolumeName == "local-registry"
			}),
		).
		Return(nil).
		Once()

	expectContainerList(harness.docker, runningSummary())

	regResult, err := harness.svc.Create(
		context.Background(),
		registry.CreateOptions{Name: "local-registry", Port: 5000},
	)
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:5000", regResult.Endpoint)
	assert.Equal(t, int32(5000), regResult.Port)
	assert.Equal(t, "local-registry-data", regResult.VolumeName)
	assert.Equal(t, v1alpha1.OCIRegistryStatusRunning, regResult.Status)
}

func TestStartStartsAndConnectsRegistry(t *testing.T) {
	t.Parallel()

	harness := newRegistryTestHarness(t)

	expectContainerList(harness.docker, exitedSummary())

	harness.docker.
		On("ContainerStart", mock.Anything, "registry-id", mock.Anything).
		Return(nil).
		Once()

	networkSettings := &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{}}
	inspectResponse := container.InspectResponse{NetworkSettings: networkSettings}
	harness.docker.
		On("ContainerInspect", mock.Anything, "registry-id").
		Return(inspectResponse, nil).
		Once()

	harness.docker.
		On("NetworkConnect", mock.Anything, "kind", "registry-id", mock.Anything).
		Return(nil).
		Once()

	expectContainerList(harness.docker, runningSummary())

	regResult, err := harness.svc.Start(
		context.Background(),
		registry.StartOptions{Name: "local-registry", NetworkName: "kind"},
	)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.OCIRegistryStatusRunning, regResult.Status)
}

func TestStopHandlesScenarios(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(harness *registryTestHarness)
		opts  registry.StopOptions
	}{
		{
			name: "deletes resources when volumes should be removed",
			setup: func(harness *registryTestHarness) {
				harness.backend.
					On("DeleteRegistry", mock.Anything, "local-registry", "dev", true, "kind", "").
					Return(nil).
					Once()
			},
			opts: registry.StopOptions{
				Name:         "local-registry",
				ClusterName:  "dev",
				NetworkName:  "kind",
				DeleteVolume: true,
			},
		},
		{
			name: "removes registry container but keeps volume",
			setup: func(harness *registryTestHarness) {
				harness.backend.
					On("DeleteRegistry", mock.Anything, "local-registry", "dev", false, "kind", "").
					Return(nil).
					Once()
			},
			opts: registry.StopOptions{
				Name:        "local-registry",
				ClusterName: "dev",
				NetworkName: "kind",
			},
		},
		{
			name: "ignores missing registry",
			setup: func(harness *registryTestHarness) {
				harness.backend.
					On("DeleteRegistry", mock.Anything, "local-registry", "dev", false, "kind", "").
					Return(dockerclient.ErrRegistryNotFound).
					Once()
			},
			opts: registry.StopOptions{
				Name:        "local-registry",
				ClusterName: "dev",
				NetworkName: "kind",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := newRegistryTestHarness(t)
			if testCase.setup != nil {
				testCase.setup(&harness)
			}

			require.NoError(t, harness.svc.Stop(context.Background(), testCase.opts))
		})
	}
}

func TestStatusReturnsNotProvisionedWhenMissing(t *testing.T) {
	t.Parallel()

	harness := newRegistryTestHarness(t)

	expectContainerList(harness.docker)

	regResult, err := harness.svc.Status(
		context.Background(),
		registry.StatusOptions{Name: "local-registry"},
	)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.OCIRegistryStatusNotProvisioned, regResult.Status)
}

func wrapMockError(action string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("registry backend mock %s failure: %w", action, err)
}

func expectContainerList(
	docker *dockerclient.MockAPIClient,
	summaries ...container.Summary,
) {
	docker.
		On("ContainerList", mock.Anything, mock.Anything).
		Return(summaries, nil).
		Once()
}

func runningSummary() container.Summary {
	return container.Summary{
		ID:    "registry-id",
		State: "running",
		Ports: []container.Port{
			{PrivatePort: dockerclient.DefaultRegistryPort, PublicPort: 5000, IP: "127.0.0.1"},
		},
		Mounts: []container.MountPoint{{Type: "volume", Name: "local-registry-data"}},
		Labels: map[string]string{dockerclient.RegistryLabelKey: "local-registry"},
	}
}

func exitedSummary() container.Summary {
	summary := runningSummary()
	summary.State = "exited"

	return summary
}
