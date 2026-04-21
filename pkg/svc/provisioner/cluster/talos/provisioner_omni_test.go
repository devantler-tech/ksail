package talosprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	omniprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInMemStateForOmniTest() state.State {
	return state.WrapCore(namespaced.NewState(inmem.Build))
}

// newOmniProviderWithVersions creates a test provider with TalosVersion resources seeded in state.
func newOmniProviderWithVersions(t *testing.T, versions ...string) *omniprovider.Provider {
	t.Helper()

	testState := newInMemStateForOmniTest()

	for _, v := range versions {
		tv := omnires.NewTalosVersion(v)
		tv.TypedSpec().Value.CompatibleKubernetesVersions = []string{"1.31.0", "1.32.0"}

		require.NoError(t, testState.Create(context.Background(), tv))
	}

	return omniprovider.NewProviderWithState(testState)
}

func TestResolveOmniVersions_NoOpts(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)
	prov := newOmniProviderWithVersions(t, "1.11.2", "1.12.4")

	talosVersion, kubernetesVersion, err := provisioner.ResolveOmniVersionsForTest(
		context.Background(),
		prov,
	)

	require.NoError(t, err)
	// Should pick the latest available version from Omni
	assert.Equal(t, "1.12.4", talosVersion)
	assert.Equal(t, "1.32.0", kubernetesVersion)
}

func TestResolveOmniVersions_WithOmniOpts(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			TalosVersion:      "v1.11.2",
			KubernetesVersion: "v1.32.0",
		})

	// Provider doesn't matter when opts already set
	prov := omniprovider.NewProvider(nil)
	talosVersion, kubernetesVersion, err := provisioner.ResolveOmniVersionsForTest(
		context.Background(),
		prov,
	)

	require.NoError(t, err)
	assert.Equal(t, "v1.11.2", talosVersion)
	assert.Equal(t, "v1.32.0", kubernetesVersion)
}

func TestResolveOmniVersions_KubernetesVersionFallsBackToConfigs(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			TalosVersion: "v1.11.2",
			// KubernetesVersion intentionally empty — should fall back to talosConfigs
		})

	prov := omniprovider.NewProvider(nil)
	talosVersion, kubernetesVersion, err := provisioner.ResolveOmniVersionsForTest(
		context.Background(),
		prov,
	)

	require.NoError(t, err)
	assert.Equal(t, "v1.11.2", talosVersion)
	// Should have resolved from talosConfigs (non-empty)
	assert.NotEmpty(t, kubernetesVersion)
}

func TestResolveOmniVersions_NilClientReturnsError(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)
	// No TalosVersion opts and nil provider → should fail querying Omni
	prov := omniprovider.NewProvider(nil)

	_, _, err := provisioner.ResolveOmniVersionsForTest(context.Background(), prov)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestBuildOmniPatchInfos_NilConfigs(t *testing.T) {
	t.Parallel()

	// Provisioner with nil talosConfigs
	provisioner := talosprovisioner.NewProvisioner(nil, nil)

	patches := provisioner.BuildOmniPatchInfosForTest()

	assert.Nil(t, patches)
}

func TestBuildOmniPatchInfos_EmptyPatches(t *testing.T) {
	t.Parallel()

	// TalosConfigs with no patch files — patch list should be empty/nil
	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)

	patches := provisioner.BuildOmniPatchInfosForTest()

	assert.Empty(t, patches)
}

func TestBuildOmniPatchInfos_WithPatches(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigsWithPatches(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)

	patches := provisioner.BuildOmniPatchInfosForTest()

	require.NotEmpty(t, patches)

	for _, patch := range patches {
		assert.NotEmpty(t, patch.Path)
		assert.NotNil(t, patch.Content)
		// Scope must be one of the three valid values
		isValid := patch.Scope == omniprovider.PatchScopeCluster ||
			patch.Scope == omniprovider.PatchScopeControlPlane ||
			patch.Scope == omniprovider.PatchScopeWorker
		assert.True(t, isValid, "unexpected patch scope: %v", patch.Scope)
	}
}

func TestSyncAndWaitOmniCluster_InvalidTemplate(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)
	nilClientProv := omniprovider.NewProvider(nil)

	// Empty TalosVersion causes BuildClusterTemplate to return ErrTalosVersionRequired
	err := provisioner.SyncAndWaitOmniClusterForTest(
		context.Background(),
		nilClientProv,
		omniprovider.TemplateParams{
			ClusterName:       "test",
			TalosVersion:      "",
			KubernetesVersion: "v1.32.0",
			ControlPlanes:     1,
		},
	)

	require.Error(t, err)
	require.ErrorIs(t, err, omniprovider.ErrTalosVersionRequired)
}

func TestSyncAndWaitOmniCluster_NilClientProviderFails(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)
	nilClientProv := omniprovider.NewProvider(nil)

	// Valid template but nil-client provider: CreateCluster returns ErrProviderUnavailable
	err := provisioner.SyncAndWaitOmniClusterForTest(
		context.Background(),
		nilClientProv,
		omniprovider.TemplateParams{
			ClusterName:       "test",
			TalosVersion:      "v1.11.2",
			KubernetesVersion: "v1.32.0",
			ControlPlanes:     1,
			MachineClass:      "test-class",
		},
	)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestSaveOmniKubeconfig_NilClient(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().WithKubeconfigPath(tmpDir+"/kube.yaml"),
	)
	nilClientProv := omniprovider.NewProvider(nil)

	err := provisioner.SaveOmniKubeconfigForTest(
		context.Background(),
		nilClientProv,
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestSaveOmniTalosconfig_NilClient(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().WithTalosconfigPath(tmpDir+"/talos.yaml"),
	)
	nilClientProv := omniprovider.NewProvider(nil)

	err := provisioner.SaveOmniTalosconfigForTest(
		context.Background(),
		nilClientProv,
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestGetOmniNodesByRole_NilClient(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithInfraProvider(omniprovider.NewProvider(nil))

	nodes, err := provisioner.GetOmniNodesByRoleForTest(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, nodes)
}

func TestResolveOmniMachines_MachineClassSet(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			MachineClass: "my-class",
		})

	// Provider doesn't matter when machineClass is set
	prov := omniprovider.NewProvider(nil)
	machines, err := provisioner.ResolveOmniMachinesForTest(context.Background(), prov)

	require.NoError(t, err)
	assert.Nil(t, machines)
}

func TestResolveOmniMachines_MachinesSet(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			Machines: []string{"uuid-1", "uuid-2"},
		})

	// Provider doesn't matter when machines are explicitly set
	prov := omniprovider.NewProvider(nil)
	machines, err := provisioner.ResolveOmniMachinesForTest(context.Background(), prov)

	require.NoError(t, err)
	assert.Equal(t, []string{"uuid-1", "uuid-2"}, machines)
}

func TestResolveOmniMachines_NeitherSet_AutoDiscovers(t *testing.T) {
	t.Parallel()

	testState := newInMemStateForOmniTest()

	// Seed 2 available machines
	for _, id := range []string{"avail-1", "avail-2"} {
		ms := omnires.NewMachineStatus(id)
		ms.Metadata().Labels().Set(omnires.MachineStatusLabelAvailable, "")

		require.NoError(t, testState.Create(context.Background(), ms))
	}

	prov := omniprovider.NewProviderWithState(testState)

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().
			WithControlPlaneNodes(1).
			WithWorkerNodes(1),
	).WithOmniOptions(v1alpha1.OptionsOmni{})

	machines, err := provisioner.ResolveOmniMachinesForTest(context.Background(), prov)

	require.NoError(t, err)
	assert.Len(t, machines, 2)
}

func TestResolveOmniMachines_NeitherSet_InsufficientAvailable(t *testing.T) {
	t.Parallel()

	testState := newInMemStateForOmniTest()

	// Seed only 1 available machine but need 3
	ms := omnires.NewMachineStatus("avail-1")
	ms.Metadata().Labels().Set(omnires.MachineStatusLabelAvailable, "")

	require.NoError(t, testState.Create(context.Background(), ms))

	prov := omniprovider.NewProviderWithState(testState)

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().
			WithControlPlaneNodes(1).
			WithWorkerNodes(2),
	).WithOmniOptions(v1alpha1.OptionsOmni{})

	machines, err := provisioner.ResolveOmniMachinesForTest(context.Background(), prov)

	require.Error(t, err)
	require.ErrorIs(t, err, omniprovider.ErrInsufficientAvailableMachines)
	assert.Nil(t, machines)
}

func TestResolveOmniMachines_NilOmniOpts(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	// No WithOmniOptions — omniOpts is nil
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().
			WithControlPlaneNodes(1),
	)

	testState := newInMemStateForOmniTest()

	ms := omnires.NewMachineStatus("avail-1")
	ms.Metadata().Labels().Set(omnires.MachineStatusLabelAvailable, "")

	require.NoError(t, testState.Create(context.Background(), ms))

	prov := omniprovider.NewProviderWithState(testState)

	machines, err := provisioner.ResolveOmniMachinesForTest(context.Background(), prov)

	require.NoError(t, err)
	assert.Len(t, machines, 1)
}

func TestResolveOmniMachines_BothSet_ReturnsConflict(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			MachineClass: "my-class",
			Machines:     []string{"uuid-1"},
		})

	prov := omniprovider.NewProvider(nil)
	machines, err := provisioner.ResolveOmniMachinesForTest(context.Background(), prov)

	require.Error(t, err)
	require.ErrorIs(t, err, omniprovider.ErrMachineAllocationConflict)
	assert.Nil(t, machines)
}

func TestSaveOmniConfig_WritesFile(t *testing.T) {
	t.Parallel()

	// Use a temp dir so tilde expansion is not needed, but verify saveOmniConfig
	// actually writes the file and handles path expansion/canonicalization.
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "subdir", "test-kube.yaml")

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().WithKubeconfigPath(outPath),
	)

	dummyData := []byte("apiVersion: v1\nkind: Config\n")
	err := provisioner.SaveOmniConfigForTest(dummyData, outPath, "Kubeconfig")
	require.NoError(t, err)

	written, err := os.ReadFile(outPath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	assert.Equal(t, dummyData, written)
}

func TestRenameKubeconfigContext(t *testing.T) {
	t.Parallel()

	// Core renaming logic is tested in pkg/k8s/kubeconfig_test.go.
	// This test verifies the test seam delegates correctly.
	validKubeconfig := `apiVersion: v1
kind: Config
current-context: devantler-devantler-dev-ksail
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: devantler-devantler-dev-ksail
contexts:
- context:
    cluster: devantler-devantler-dev-ksail
    user: devantler-devantler-dev-ksail
  name: devantler-devantler-dev-ksail
users:
- name: devantler-devantler-dev-ksail
  user:
    token: test-token
`

	result, err := talosprovisioner.RenameKubeconfigContextForTest(
		[]byte(validKubeconfig), "admin@devantler-dev",
	)

	require.NoError(t, err)
	assert.Contains(t, string(result), "current-context: admin@devantler-dev")
	assert.Contains(t, string(result), "name: admin@devantler-dev")
	assert.NotContains(t, string(result), "devantler-devantler-dev-ksail")
}

// TestRefreshOmniConfigsIfNeeded_NilInfraProvider verifies that when no infra
// provider is set (infraProvider == nil) the refresh is skipped without error.
// This covers the non-Omni fast-path for Docker/Hetzner provisioners.
func TestRefreshOmniConfigsIfNeeded_NilInfraProvider(t *testing.T) {
	t.Parallel()

	// No WithInfraProvider call → infraProvider stays nil.
	p := talosprovisioner.NewProvisioner(nil, nil)

	err := p.RefreshOmniConfigsIfNeededForTest(context.Background(), "demo")

	require.NoError(t, err, "nil infraProvider must be a no-op")
}

// TestRefreshOmniConfigsIfNeeded_NonOmniProvider verifies that when the infra
// provider is not an *omniprovider.Provider the type assertion returns false and
// the refresh is silently skipped, returning nil.
func TestRefreshOmniConfigsIfNeeded_NonOmniProvider(t *testing.T) {
	t.Parallel()

	// provider.MockProvider satisfies the provider.Provider interface but is not
	// *omniprovider.Provider, so the type assertion should return (nil, false).
	mockProv := provider.NewMockProvider()

	p := talosprovisioner.NewProvisioner(nil, nil).WithInfraProvider(mockProv)

	err := p.RefreshOmniConfigsIfNeededForTest(context.Background(), "demo")

	require.NoError(t, err, "non-Omni infraProvider must be a no-op")
	// No calls should be made on the mock since the path is skipped.
	mockProv.AssertNotCalled(t, "ListNodes")
}

// TestRefreshOmniConfigsIfNeeded_OmniProvider_NoPaths verifies that when the
// infra provider IS an *omniprovider.Provider but no kubeconfig or talosconfig
// output path is configured, saveOmniConfigs is effectively a no-op and returns nil.
func TestRefreshOmniConfigsIfNeeded_OmniProvider_NoPaths(t *testing.T) {
	t.Parallel()

	// No output paths set; saveOmniConfigs will skip both branches and return nil.
	omniProv := omniprovider.NewProvider(nil)

	p := talosprovisioner.NewProvisioner(nil, nil).WithInfraProvider(omniProv)

	err := p.RefreshOmniConfigsIfNeededForTest(context.Background(), "demo")

	require.NoError(t, err, "Omni provider with no output paths must be a no-op")
}

// TestRefreshOmniConfigsIfNeeded_OmniProvider_WithKubeconfigPath verifies that
// when the infra provider is an *omniprovider.Provider and a kubeconfig output
// path is configured, refreshOmniConfigsIfNeeded forwards to saveOmniKubeconfig.
// The nil-client Omni provider returns ErrProviderUnavailable, proving that the
// refresh code path is reached rather than silently skipped.
func TestRefreshOmniConfigsIfNeeded_OmniProvider_WithKubeconfigPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	omniProv := omniprovider.NewProvider(nil) // nil client → ErrProviderUnavailable

	p := talosprovisioner.NewProvisioner(nil,
		talosprovisioner.NewOptions().WithKubeconfigPath(filepath.Join(tmpDir, "kube.yaml")),
	).WithInfraProvider(omniProv)

	err := p.RefreshOmniConfigsIfNeededForTest(context.Background(), "demo")

	require.Error(t, err, "expected an error from nil-client Omni provider")
	require.ErrorIs(t, err, provider.ErrProviderUnavailable,
		"error must propagate from saveOmniKubeconfig as ErrProviderUnavailable")
}

// TestRefreshOmniConfigsIfNeeded_OmniProvider_WithTalosconfigPath mirrors
// TestRefreshOmniConfigsIfNeeded_OmniProvider_WithKubeconfigPath but for the
// talosconfig output path, verifying both branches of saveOmniConfigs are reachable.
func TestRefreshOmniConfigsIfNeeded_OmniProvider_WithTalosconfigPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	omniProv := omniprovider.NewProvider(nil)

	p := talosprovisioner.NewProvisioner(nil,
		talosprovisioner.NewOptions().WithTalosconfigPath(filepath.Join(tmpDir, "talos.yaml")),
	).WithInfraProvider(omniProv)

	err := p.RefreshOmniConfigsIfNeededForTest(context.Background(), "demo")

	require.Error(t, err, "expected an error from nil-client Omni provider")
	require.ErrorIs(t, err, provider.ErrProviderUnavailable,
		"error must propagate from saveOmniTalosconfig as ErrProviderUnavailable")
}

// TestRefreshOmniConfigsIfNeeded_TypedNilOmniProvider verifies that when the
// infra provider interface holds a typed-nil *omniprovider.Provider (ok==true,
// omniProv==nil), refreshOmniConfigsIfNeeded treats it as a no-op and returns
// nil rather than panicking with a nil-pointer dereference inside saveOmniConfigs.
func TestRefreshOmniConfigsIfNeeded_TypedNilOmniProvider(t *testing.T) {
	t.Parallel()

	var typedNil *omniprovider.Provider // typed-nil: ok==true, omniProv==nil

	p := talosprovisioner.NewProvisioner(nil,
		talosprovisioner.NewOptions().WithKubeconfigPath("/tmp/should-not-be-written"),
	).WithInfraProvider(typedNil)

	err := p.RefreshOmniConfigsIfNeededForTest(context.Background(), "demo")

	require.NoError(t, err, "typed-nil Omni provider must be treated as a no-op, not panic")
}
