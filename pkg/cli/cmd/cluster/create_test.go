package cluster_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/docker/docker/client"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
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
) (clusterprovisioner.ClusterProvisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeProvisioner{}, cfg, nil
}

type fakeInstaller struct{ called bool }

func (f *fakeInstaller) Install(context.Context) error {
	f.called = true

	return nil
}

func (*fakeInstaller) Uninstall(context.Context) error { return nil }

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

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func writeTestConfigFiles(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
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
	// Create a fake kubeconfig file to prevent errors when ArgoCD tries to create Helm client
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)
}

func newTestRuntimeContainer(t *testing.T) *runtime.Runtime {
	t.Helper()

	return runtime.New(
		func(i runtime.Injector) error {
			do.Provide(i, func(runtime.Injector) (timer.Timer, error) {
				return timer.New(), nil
			})

			return nil
		},
		func(i runtime.Injector) error {
			do.Provide(i, func(runtime.Injector) (clusterprovisioner.Factory, error) {
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

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{})
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

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{})
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

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if factoryCalled {
		t.Fatalf("expected cert-manager installer factory not to be invoked")
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

//nolint:nlreturn // keep inline returns in short closures for function length
func setupGitOpsTestMocks(
	t *testing.T,
	engine v1alpha1.GitOpsEngine,
) (func() *fakeInstaller, *bool) {
	t.Helper()

	var fake *fakeInstaller

	ensureCalled := false

	// Override cluster provisioner factory to use fake provisioner
	t.Cleanup(clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{}))

	// Set up the appropriate installer and ensure mocks based on the GitOps engine
	switch engine {
	case v1alpha1.GitOpsEngineArgoCD:
		t.Cleanup(clusterpkg.SetArgoCDInstallerFactoryForTests(
			func(_ *v1alpha1.Cluster) (installer.Installer, error) {
				fake = &fakeInstaller{}
				return fake, nil
			},
		))
		t.Cleanup(clusterpkg.SetEnsureArgoCDResourcesForTests(
			func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string) error {
				ensureCalled = true
				return nil
			},
		))
	case v1alpha1.GitOpsEngineFlux:
		t.Cleanup(clusterpkg.SetFluxInstallerFactoryForTests(
			func(_ *v1alpha1.Cluster) (installer.Installer, error) {
				fake = &fakeInstaller{}
				return fake, nil
			},
		))
		t.Cleanup(clusterpkg.SetEnsureFluxResourcesForTests(
			func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string) error {
				ensureCalled = true
				return nil
			},
		))
	case v1alpha1.GitOpsEngineNone:
		t.Fatalf("GitOpsEngineNone is not supported in this test helper")
	}

	// Mock registry service factory to avoid needing a real Docker client
	t.Cleanup(clusterpkg.SetLocalRegistryServiceFactoryForTests(fakeRegistryServiceFactory))

	t.Cleanup(clusterpkg.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, fn func(client.APIClient) error) error {
			return fn(nil) // Call the callback to trigger success messages
		},
	))

	return func() *fakeInstaller { return fake }, &ensureCalled
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
func TestCreate_LocalPathStorageCSI_InstallsOnKind(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    csi: LocalPathStorage
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
	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	fake := &fakeInstaller{}

	restore := clusterpkg.SetCSIInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fake, nil
		},
	)
	defer restore()

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

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

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

// TestCreate_Minimal_PrintsOnlyClusterLifecycle tests cluster creation with no extras.
// This verifies the minimal output when all optional components are disabled.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_Minimal_PrintsOnlyClusterLifecycle(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	// Override Docker client to call the callback for success messages
	restoreDocker := clusterpkg.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, fn func(client.APIClient) error) error {
			return fn(nil) // Call the callback to trigger success messages
		},
	)
	defer restoreDocker()

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

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
	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := clusterpkg.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

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

func TestShouldPushOCIArtifact_ArgoCDShouldNotPush(t *testing.T) {
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
	require.False(t, result, "Should not push when ArgoCD is the GitOps engine")
}

func TestShouldPushOCIArtifact_NoLocalRegistryShouldNotPush(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
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
	_ clusterprovisioner.ClusterProvisioner = (*fakeProvisioner)(nil)
	_ clusterprovisioner.Factory            = (*fakeFactory)(nil)
	_ installer.Installer                   = (*fakeInstaller)(nil)
)
