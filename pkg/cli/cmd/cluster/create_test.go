package cluster_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	dockerpkg "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

type fakeProvisioner struct{}

func (*fakeProvisioner) Create(context.Context, string) error { return nil }
func (*fakeProvisioner) Delete(context.Context, string) error { return nil }
func (*fakeProvisioner) Start(context.Context, string) error  { return nil }
func (*fakeProvisioner) Stop(context.Context, string) error   { return nil }
func (*fakeProvisioner) List(context.Context) ([]string, error) {
	return nil, nil
}
func (*fakeProvisioner) Exists(context.Context, string) (bool, error) { return true, nil }

type fakeFactory struct{}

func (fakeFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeProvisioner{}, cfg, nil
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
		func(_ client.APIClient) (registry.Backend, error) {
			return mockBackend, nil
		},
	))

	// Mock the Docker client invoker to use a mock Docker API client.
	// This calls the callback with a mock client so stages execute and print output.
	t.Cleanup(clusterpkg.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, fn func(client.APIClient) error) error {
			mockClient := newMockDockerClient(t)

			return fn(mockClient)
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
	// Create a fake kubeconfig file to prevent errors when ArgoCD tries to create Helm client
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)
}

func newTestRuntimeContainer(t *testing.T) *di.Runtime {
	t.Helper()

	return di.New(
		func(i di.Injector) error {
			do.Provide(i, func(di.Injector) (timer.Timer, error) {
				return timer.New(), nil
			})

			return nil
		},
		func(i di.Injector) error {
			do.Provide(i, func(di.Injector) (clusterprovisioner.Factory, error) {
				return fakeFactory{}, nil
			})

			return nil
		},
	)
}

// trimTrailingNewline removes a single trailing newline from snapshot output.
// This produces cleaner snapshot comparisons.
func trimTrailingNewline(s string) string {
	return strings.TrimSuffix(s, "\n")
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EnabledCertManager_PrintsInstallStage(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	fake := &fakeInstaller{}

	restore := clusterpkg.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fake, nil
		},
	)
	defer restore()

	testRuntime := newTestRuntimeContainer(t)

	cmd := clusterpkg.NewCreateCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cert-manager", "Enabled"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if !fake.called {
		t.Fatalf("expected cert-manager installer to be invoked")
	}

	// Normalize timing variance: keep --timing disabled in this test.
	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_DefaultCertManager_DoesNotInstall(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	factoryCalled := false

	restore := clusterpkg.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			factoryCalled = true

			return &fakeInstaller{}, nil
		},
	)
	defer restore()

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if factoryCalled {
		t.Fatalf("expected cert-manager installer factory not to be invoked")
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

func setupGitOpsTestMocks(
	t *testing.T,
	engine v1alpha1.GitOpsEngine,
) (func() *fakeInstaller, *bool) {
	t.Helper()

	var fake *fakeInstaller

	ensureCalled := false

	// Override cluster provisioner factory to use fake provisioner
	t.Cleanup(clusterpkg.SetProvisionerFactoryForTests(fakeFactory{}))

	// Set up the appropriate installer and ensure mocks based on the GitOps engine
	switch engine {
	case v1alpha1.GitOpsEngineArgoCD:
		setupArgoCDMocks(t, &fake, &ensureCalled)
	case v1alpha1.GitOpsEngineFlux:
		setupFluxMocks(t, &fake, &ensureCalled)
	case v1alpha1.GitOpsEngineNone:
		t.Fatalf("GitOpsEngineNone is not supported in this test helper")
	}

	// Mock registry service factory to avoid needing a real Docker client
	t.Cleanup(clusterpkg.SetLocalRegistryServiceFactoryForTests(fakeRegistryServiceFactory))

	// Note: DockerClientInvoker is NOT overridden here - tests should call
	// setupMockRegistryBackend() before setupGitOpsTestMocks() to configure
	// a mock Docker client that will be used for all Docker operations.

	return func() *fakeInstaller { return fake }, &ensureCalled
}

func setupArgoCDMocks(t *testing.T, fake **fakeInstaller, ensureCalled *bool) {
	t.Helper()
	t.Cleanup(clusterpkg.SetArgoCDInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			*fake = &fakeInstaller{}

			return *fake, nil
		},
	))
	t.Cleanup(clusterpkg.SetEnsureArgoCDResourcesForTests(
		func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string) error {
			*ensureCalled = true

			return nil
		},
	))
	t.Cleanup(clusterpkg.SetEnsureOCIArtifactForTests(
		func(_ context.Context, _ *cobra.Command, _ *v1alpha1.Cluster, _ string, _ io.Writer) (bool, error) {
			return true, nil
		},
	))
}

func setupFluxMocks(t *testing.T, fake **fakeInstaller, ensureCalled *bool) {
	t.Helper()
	t.Cleanup(clusterpkg.SetFluxInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			*fake = &fakeInstaller{}

			return *fake, nil
		},
	))
	t.Cleanup(clusterpkg.SetSetupFluxInstanceForTests(
		func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string) error {
			*ensureCalled = true

			return nil
		},
	))
	t.Cleanup(clusterpkg.SetWaitForFluxReadyForTests(
		func(_ context.Context, _ string) error {
			return nil
		},
	))
	t.Cleanup(clusterpkg.SetEnsureOCIArtifactForTests(
		func(_ context.Context, _ *cobra.Command, _ *v1alpha1.Cluster, _ string, _ io.Writer) (bool, error) {
			return true, nil
		},
	))
}

func TestCreate_GitOps_PrintsInstallStage(t *testing.T) {
	testCases := []struct {
		name   string
		engine v1alpha1.GitOpsEngine
		arg    string
	}{
		{name: "ArgoCD", engine: v1alpha1.GitOpsEngineArgoCD, arg: "ArgoCD"},
		{name: "Flux", engine: v1alpha1.GitOpsEngineFlux, arg: "Flux"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tmpRoot := t.TempDir()
			t.Setenv("TMPDIR", tmpRoot)

			workingDir := t.TempDir()
			t.Chdir(workingDir)
			writeTestConfigFiles(t, workingDir)
			setupMockRegistryBackend(t)

			fake, ensureCalled := setupGitOpsTestMocks(t, testCase.engine)

			testRuntime := newTestRuntimeContainer(t)

			cmd := clusterpkg.NewCreateCmd(testRuntime)

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())
			cmd.SetArgs([]string{"--gitops-engine", testCase.arg})

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
			}

			if !*ensureCalled {
				t.Fatalf("expected %s resources ensure hook to be invoked", testCase.name)
			}

			if installer := fake(); installer == nil || !installer.called {
				t.Fatalf("expected %s installer to be invoked", testCase.name)
			}

			snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
		})
	}
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_CSIEnabled_InstallsOnKind(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    csi: Enabled
    metricsServer: Disabled
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
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	fake := &fakeInstaller{}

	restore := clusterpkg.SetCSIInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fake, nil
		},
	)
	defer restore()

	setupMockRegistryBackend(t)

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if !fake.called {
		t.Fatalf("expected CSI installer to be invoked")
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_DefaultCSI_DoesNotInstall(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

// TestCreate_Minimal_PrintsOnlyClusterLifecycle tests cluster creation with no extras.
// This verifies the minimal output when all optional components are disabled.
// Uses config with localRegistry disabled to skip registry stages.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_Minimal_PrintsOnlyClusterLifecycle(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir) // Uses config with localRegistry disabled
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

// TestCreate_LocalRegistryDisabled_SkipsRegistryStages tests cluster creation with local registry disabled.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_LocalRegistryDisabled_SkipsRegistryStages(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    localRegistry:
      enabled: false
    metricsServer: Disabled
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
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	setupMockRegistryBackend(t)

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

func TestShouldPushOCIArtifact_FluxWithLocalRegistry(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := clusterpkg.ExportShouldPushOCIArtifact(clusterCfg)
	require.True(t, result, "Should push when Flux is enabled with local registry")
}

func TestShouldPushOCIArtifact_ArgoCDWithLocalRegistry(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := clusterpkg.ExportShouldPushOCIArtifact(clusterCfg)
	require.True(t, result, "Should push when ArgoCD is enabled with local registry")
}

func TestShouldPushOCIArtifact_NoLocalRegistryShouldNotPush(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					// Empty registry - disabled
				},
			},
		},
	}

	result := clusterpkg.ExportShouldPushOCIArtifact(clusterCfg)
	require.False(t, result, "Should not push when local registry is disabled")
}

func TestShouldPushOCIArtifact_NoGitOpsEngineShouldNotPush(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineNone,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := clusterpkg.ExportShouldPushOCIArtifact(clusterCfg)
	require.False(t, result, "Should not push when GitOps engine is none")
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.Provisioner = (*fakeProvisioner)(nil)
	_ clusterprovisioner.Factory     = (*fakeFactory)(nil)
	_ installer.Installer            = (*fakeInstaller)(nil)
)

func TestSetupK3dCSI_DisablesCSI(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{}

	clusterpkg.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Verify the flag was added
	found := false

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=local-storage" {
			found = true

			require.Equal(t, []string{"server:*"}, arg.NodeFilters)

			break
		}
	}

	require.True(t, found, "--disable=local-storage flag should be added")
}

func TestSetupK3dCSI_DoesNotDuplicateFlag(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{
		Options: v1alpha5.SimpleConfigOptions{
			K3sOptions: v1alpha5.SimpleConfigOptionsK3s{
				ExtraArgs: []v1alpha5.K3sArgWithNodeFilters{
					{
						Arg:         "--disable=local-storage",
						NodeFilters: []string{"server:*"},
					},
				},
			},
		},
	}

	clusterpkg.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Count occurrences of the flag
	count := 0

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=local-storage" {
			count++
		}
	}

	require.Equal(t, 1, count, "flag should not be duplicated")
}

func TestSetupK3dCSI_DoesNothingForNonK3s(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{}

	clusterpkg.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Verify no flags were added
	require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
}

func TestSetupK3dCSI_DoesNothingWhenCSINotDisabled(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		csi  v1alpha1.CSI
	}{
		{"default", v1alpha1.CSIDefault},
		{"enabled", v1alpha1.CSIEnabled},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						CSI:          testCase.csi,
					},
				},
			}

			k3dConfig := &v1alpha5.SimpleConfig{}

			clusterpkg.ExportSetupK3dCSI(clusterCfg, k3dConfig)

			// Verify no flags were added
			require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
		})
	}
}

func TestResolveClusterNameFromContext_Vanilla(t *testing.T) {
	t.Parallel()

	kindConfig := &v1alpha4.Cluster{Name: "kind-cluster"}
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
				},
			},
		},
		KindConfig: kindConfig,
	}

	name := clusterpkg.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "kind-cluster", name)
}

func TestResolveClusterNameFromContext_K3s(t *testing.T) {
	t.Parallel()

	k3dConfig := &v1alpha5.SimpleConfig{
		ObjectMeta: types.ObjectMeta{
			Name: "k3s-cluster",
		},
	}
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionK3s,
				},
			},
		},
		K3dConfig: k3dConfig,
	}

	name := clusterpkg.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "k3s-cluster", name)
}

func TestResolveClusterNameFromContext_FallbackToContext(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.Distribution("unknown"),
					Connection: v1alpha1.Connection{
						Context: "custom-context",
					},
				},
			},
		},
	}

	name := clusterpkg.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "custom-context", name)
}

func TestResolveClusterNameFromContext_FallbackToDefault(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.Distribution("unknown"),
				},
			},
		},
	}

	name := clusterpkg.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "ksail", name)
}
