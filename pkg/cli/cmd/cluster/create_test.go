package cluster_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/timer"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
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

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func writeTestConfigFiles(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Kind
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

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cert-manager", "Enabled"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, buf.String())
	}

	if !fake.called {
		t.Fatalf("expected cert-manager installer to be invoked")
	}

	// Normalize timing variance: keep --timing disabled in this test.
	snaps.MatchSnapshot(t, buf.String())
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

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, buf.String())
	}

	if factoryCalled {
		t.Fatalf("expected cert-manager installer factory not to be invoked")
	}

	require.NotContains(t, buf.String(), "Install Cert-Manager...")
}

func setupArgoCDTestMocks(t *testing.T) (func() *fakeInstaller, *bool) {
	t.Helper()

	var fake *fakeInstaller

	ensureCalled := false

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(fakeFactory{})
	t.Cleanup(restoreFactory)

	restoreInstaller := clusterpkg.SetArgoCDInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			fake = &fakeInstaller{}

			return fake, nil
		},
	)
	t.Cleanup(restoreInstaller)

	restoreEnsure := clusterpkg.SetEnsureArgoCDResourcesForTests(
		func(_ context.Context, _ string, _ *v1alpha1.Cluster) error {
			ensureCalled = true

			return nil
		},
	)
	t.Cleanup(restoreEnsure)

	restoreDocker := clusterpkg.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, _ func(client.APIClient) error) error {
			return nil
		},
	)
	t.Cleanup(restoreDocker)

	return func() *fakeInstaller { return fake }, &ensureCalled
}

func TestCreate_ArgoCD_PrintsInstallStage(t *testing.T) {
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)

	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)

	fake, ensureCalled := setupArgoCDTestMocks(t)

	testRuntime := newTestRuntimeContainer(t)

	cmd := clusterpkg.NewCreateCmd(testRuntime)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--gitops-engine", "ArgoCD"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, buf.String())
	}

	// We can only reliably assert the ensure hook was invoked directly.
	// The installer invocation is verified indirectly via the overall command output snapshot below,
	// not through a separate snapshot of the installer invocation itself.
	if !*ensureCalled {
		t.Fatalf("expected Argo CD resources ensure hook to be invoked")
	}

	if installer := fake(); installer == nil || !installer.called {
		t.Fatalf("expected Argo CD installer to be invoked")
	}

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_LocalPathStorageCSI_InstallsOnKind(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Kind
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

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, buf.String())
	}

	if !fake.called {
		t.Fatalf("expected CSI installer to be invoked")
	}

	require.Contains(t, buf.String(), "csi installing")
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

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, buf.String())
	}

	require.NotContains(t, buf.String(), "csi installing")
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.ClusterProvisioner = (*fakeProvisioner)(nil)
	_ clusterprovisioner.Factory            = (*fakeFactory)(nil)
	_ installer.Installer                   = (*fakeInstaller)(nil)
)
