package cluster_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// disableTraefikArg is the K3s argument to disable the built-in Traefik ingress controller.
const disableTraefikArg = "--disable=traefik"

type fakeProvisioner struct{ createName *string }

func (p *fakeProvisioner) Create(_ context.Context, name string) error {
	if p.createName != nil {
		*p.createName = name
	}

	return nil
}

func (*fakeProvisioner) Delete(context.Context, string) error { return nil }

func (*fakeProvisioner) Start(context.Context, string) error { return nil }

func (*fakeProvisioner) Stop(context.Context, string) error { return nil }

func (*fakeProvisioner) List(context.Context) ([]string, error) {
	return nil, nil
}

func (*fakeProvisioner) Exists(context.Context, string) (bool, error) { return true, nil }

type fakeFactory struct{ createName *string }

func (f fakeFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeProvisioner{createName: f.createName}, cfg, nil
}

type fakeInstaller struct{ called bool }

func (f *fakeInstaller) Install(context.Context) error {
	f.called = true

	return nil
}

func (*fakeInstaller) Uninstall(context.Context) error { return nil }

func (*fakeInstaller) Images(context.Context) ([]string, error) { return nil, nil }

// fakeRegistryService is a mock registry service for testing.
type fakeRegistryService struct{}

func (*fakeRegistryService) Create(
	_ context.Context,
	_ registry.CreateOptions,
) (v1alpha1.OCIRegistry, error) {
	return v1alpha1.NewOCIRegistry(), nil
}

func (*fakeRegistryService) Start(
	_ context.Context,
	_ registry.StartOptions,
) (v1alpha1.OCIRegistry, error) {
	return v1alpha1.NewOCIRegistry(), nil
}

func (*fakeRegistryService) Stop(_ context.Context, _ registry.StopOptions) error {
	return nil
}

func (*fakeRegistryService) Status(
	_ context.Context,
	_ registry.StatusOptions,
) (v1alpha1.OCIRegistry, error) {
	return v1alpha1.NewOCIRegistry(), nil
}

func fakeRegistryServiceFactory(_ registry.Config) (registry.Service, error) {
	return &fakeRegistryService{}, nil
}

// newMockDockerClient creates a mock Docker API client for use in tests.
// It stubs all commonly-used Docker operations to succeed as no-ops.
func newMockDockerClient(t *testing.T) *dockerpkg.MockAPIClient {
	t.Helper()

	mockClient := dockerpkg.NewMockAPIClient(t)

	// Network operations - return empty/success
	mockClient.EXPECT().
		NetworkList(mock.Anything, mock.Anything).
		Return([]network.Summary{}, nil).Maybe()
	mockClient.EXPECT().
		NetworkCreate(mock.Anything, mock.Anything, mock.Anything).
		Return(network.CreateResponse{}, nil).Maybe()
	mockClient.EXPECT().
		NetworkRemove(mock.Anything, mock.Anything).
		Return(nil).Maybe()
	mockClient.EXPECT().
		NetworkConnect(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()

	// Container operations - return empty list (no existing containers)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).
		Maybe()

	// Close operation - succeed
	mockClient.EXPECT().Close().Return(nil).Maybe()

	return mockClient
}

// setupMockRegistryBackend configures a mock registry backend that doesn't create real containers.
// Call this in tests to enable default mirror registries (docker.io, ghcr.io) without Docker.
// This also mocks the Docker client invoker to use a mock Docker API client.
//
// IMPORTANT: Call this BEFORE other test setup helpers (like setupGitOpsTestMocks) to ensure
// the mock Docker client is properly configured for all Docker operations.
func setupMockRegistryBackend(t *testing.T) {
	t.Helper()

	mockBackend := registry.NewMockBackend(t)
	// Allow any calls to ListRegistries - returns empty list (no existing registries)
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{}, nil).Maybe()
	// Allow any calls to GetRegistryPort - returns 0, not found (no existing registries)
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, mock.Anything).Return(0, nil).Maybe()
	// Allow any calls to CreateRegistry - succeeds (no-op in tests)
	mockBackend.EXPECT().CreateRegistry(mock.Anything, mock.Anything).Return(nil).Maybe()
	// Allow any calls to DeleteRegistry - succeeds (no-op in tests)
	mockBackend.EXPECT().
		DeleteRegistry(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Maybe()
	// Allow any calls to WaitForRegistriesReady - succeeds immediately (no-op in tests)
	mockBackend.EXPECT().WaitForRegistriesReady(mock.Anything, mock.Anything).Return(nil).Maybe()

	t.Cleanup(registry.SetBackendFactoryForTests(
		func(_ dockerpkg.Client) (registry.Backend, error) {
			return mockBackend, nil
		},
	))

	// Mock the Docker client invoker to use a mock Docker API client.
	// This calls the callback with a mock client so stages execute and print output.
	t.Cleanup(cluster.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, fn func(dockerpkg.Client) error) error {
			mockClient := newMockDockerClient(t)

			return fn(mockClient)
		},
	))

	// Override the cluster stability check to be a no-op.
	// Tests use a fake kubeconfig without a real cluster, so the real
	// stability check would time out waiting for API server connectivity.
	t.Cleanup(cluster.SetClusterStabilityCheckForTests(
		func(_ context.Context, _ *v1alpha1.Cluster, _ bool) error {
			return nil
		},
	))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// writeTestConfigFiles writes test config files with local registry disabled.
// This produces minimal output without needing Docker client mocking.
func writeTestConfigFiles(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    metricsServer: Disabled
    localRegistry:
      enabled: false
    connection:
      kubeconfig: ./kubeconfig
`

	writeFile(t, workingDir, "ksail.yaml", ksailYAML)
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n",
	)
	// Create a fake kubeconfig file with the expected context entry to prevent
	// validation errors when ArgoCD tries to create a Helm client.
	// Vanilla distribution with kind.yaml name "test" → context "kind-test".
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\ncurrent-context: kind-test\nclusters:\n"+
			"- cluster:\n    server: https://127.0.0.1:6443\n  name: kind-test\n"+
			"contexts:\n- context:\n    cluster: kind-test\n    user: kind-test\n  name: kind-test\n"+
			"users:\n- name: kind-test\n  user:\n    token: fake\n",
	)
}

// trimTrailingNewline removes a single trailing newline from snapshot output.
// This produces cleaner snapshot comparisons.
func trimTrailingNewline(s string) string {
	return strings.TrimSuffix(s, "\n")
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.Provisioner = (*fakeProvisioner)(nil)
	_ clusterprovisioner.Factory     = (*fakeFactory)(nil)
	_ installer.Installer            = (*fakeInstaller)(nil)
)

func TestMain(m *testing.M) {
	os.Exit(homeenv.RunFunc(func() int {
		return snapshottest.Run(m, snaps.CleanOpts{Sort: true})
	}))
}

var errClusterPureTalosConfigEmpty = errors.New("talos config file is empty")

const (
	testFieldHetznerCPServerType = "provider.hetzner.controlPlaneServerType"
	testFieldClusterCNI          = "cluster.cni"
)
