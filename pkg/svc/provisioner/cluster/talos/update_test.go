package talosprovisioner_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	omniprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/siderolabs/omni/client/api/omni/specs"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test with multiple node topology scenarios is clearer as single function
func TestCountNodeRoles(t *testing.T) {
	t.Parallel()

	newNode := talosprovisioner.NewNodeWithRoleForTest

	tests := []struct {
		name       string
		nodes      []talosprovisioner.NodeWithRoleForTest
		wantCP     int32
		wantWorker int32
	}{
		{
			name:       "empty node list defaults to 1 CP",
			nodes:      nil,
			wantCP:     1,
			wantWorker: 0,
		},
		{
			name: "single control-plane node",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				newNode("10.0.0.2", talosprovisioner.RoleControlPlane),
			},
			wantCP:     1,
			wantWorker: 0,
		},
		{
			name: "3 control-planes and 2 workers",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				newNode("10.0.0.2", talosprovisioner.RoleControlPlane),
				newNode("10.0.0.3", talosprovisioner.RoleControlPlane),
				newNode("10.0.0.4", talosprovisioner.RoleControlPlane),
				newNode("10.0.0.5", talosprovisioner.RoleWorker),
				newNode("10.0.0.6", talosprovisioner.RoleWorker),
			},
			wantCP:     3,
			wantWorker: 2,
		},
		{
			name: "only workers defaults CP to 1",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				newNode("10.0.0.5", talosprovisioner.RoleWorker),
			},
			wantCP:     1,
			wantWorker: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cp, worker := talosprovisioner.CountNodeRolesForTest(testCase.nodes)

			if cp != testCase.wantCP {
				t.Errorf("countNodeRoles() CP = %d, want %d", cp, testCase.wantCP)
			}

			if worker != testCase.wantWorker {
				t.Errorf("countNodeRoles() worker = %d, want %d", worker, testCase.wantWorker)
			}
		})
	}
}

func TestUpdateCallsOmniNodeScaling(t *testing.T) {
	t.Parallel()

	testState := newInMemStateForOmniTest()

	// Seed a TalosVersion so resolveOmniVersions succeeds
	tv := omnires.NewTalosVersion("1.11.2")
	tv.TypedSpec().Value.CompatibleKubernetesVersions = []string{"1.32.0"}
	require.NoError(t, testState.Create(context.Background(), tv))

	// Seed a ready ClusterStatus so WaitForClusterReady returns immediately
	cs := omnires.NewClusterStatus("demo")
	cs.TypedSpec().Value.Phase = specs.ClusterStatusSpec_RUNNING
	cs.TypedSpec().Value.Ready = true
	require.NoError(t, testState.Create(context.Background(), cs))

	omniProv := omniprovider.NewProviderWithState(testState)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			MachineClass: "test-class",
		}).
		WithInfraProvider(omniProv).
		WithLogWriter(io.Discard)

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.ControlPlanes = 1
	oldSpec.Workers = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.ControlPlanes = 2
	newSpec.Workers = 2

	result, err := provisioner.Update(
		context.Background(),
		"demo",
		oldSpec,
		newSpec,
		clusterupdate.UpdateOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, result.TotalChanges(), "expected 1 CP + 1 Worker scaling change")
}

// TestUpdateSkipsOmniInPlaceConfigApply verifies the omniOpts guard in Update() prevents
// applyInPlaceConfigChanges from pushing Talos machine configs to Omni-managed nodes.
// Non-nil talosConfigs are used so the guard (not the pre-existing talosConfigs nil-check)
// is what prevents the call — without the guard this test would fail with a Talos API error.
// Uses identical specs so no scaling is triggered, isolating the in-place config guard.
func TestUpdateSkipsOmniInPlaceConfigApply(t *testing.T) {
	t.Parallel()

	talosConfigs, err := talosconfigmanager.NewDefaultConfigs()
	if err != nil {
		t.Fatalf("NewDefaultConfigs() error = %v", err)
	}

	provisioner := talosprovisioner.NewProvisioner(talosConfigs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{}).
		WithLogWriter(io.Discard)

	// Use identical specs so no scaling delta exists — this isolates the
	// in-place config change guard for Omni clusters.
	spec := &v1alpha1.ClusterSpec{}
	spec.ControlPlanes = 1

	_, err = provisioner.Update(
		context.Background(),
		"demo",
		spec,
		spec,
		clusterupdate.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}
}

// TestUpdateDoesNotAttemptVersionUpgrade verifies that Update() does not
// implicitly attempt Talos OS version upgrades. Version upgrades are only
// triggered via the explicit --update-distribution flag which goes through
// the UpgradeDistribution() path (not Update()).
// The provisioner is instantiated WITHOUT Omni options so any accidental
// reintroduction of an upgrade step would surface as a failure (no Omni
// guard to silently skip it).
// See: https://github.com/devantler-tech/ksail/issues/4260
func TestUpdateDoesNotAttemptVersionUpgrade(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard)

	// Identical specs: no scaling/config changes — verifies that Update()
	// completes without attempting a version upgrade.
	spec := &v1alpha1.ClusterSpec{}
	spec.ControlPlanes = 1

	result, err := provisioner.Update(
		context.Background(),
		"demo",
		spec,
		spec,
		clusterupdate.UpdateOptions{},
	)
	require.NoError(t, err)
	assert.Empty(t, result.FailedChanges)
}

// TestApplyNodeScalingChanges_NilSpecs verifies that nil specs short-circuit scaling without error.
// The implementation short-circuits when either spec is nil, so all three nil combinations are tested.
func TestApplyNodeScalingChanges_NilSpecs(t *testing.T) {
	t.Parallel()

	nonNilSpec := &v1alpha1.ClusterSpec{}

	tests := []struct {
		name    string
		oldSpec *v1alpha1.ClusterSpec
		newSpec *v1alpha1.ClusterSpec
	}{
		{
			name:    "both specs nil",
			oldSpec: nil,
			newSpec: nil,
		},
		{
			name:    "old spec nil",
			oldSpec: nil,
			newSpec: nonNilSpec,
		},
		{
			name:    "new spec nil",
			oldSpec: nonNilSpec,
			newSpec: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			p := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)
			result := clusterupdate.NewEmptyUpdateResult()

			err := p.ApplyNodeScalingChangesForTest(
				context.Background(),
				"test",
				testCase.oldSpec,
				testCase.newSpec,
				result,
			)
			require.NoError(t, err)
		})
	}
}

// TestApplyNodeScalingChanges_NoDelta verifies that equal specs produce a no-op.
func TestApplyNodeScalingChanges_NoDelta(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)
	result := clusterupdate.NewEmptyUpdateResult()

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.ControlPlanes = 3
	oldSpec.Workers = 2

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.ControlPlanes = 3
	newSpec.Workers = 2

	err := provisioner.ApplyNodeScalingChangesForTest(
		context.Background(),
		"test",
		oldSpec,
		newSpec,
		result,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalChanges(), "no changes expected when deltas are zero")
}

// TestApplyNodeScalingChanges_OmniScalingIsAttempted verifies that the Omni
// provider path attempts node scaling by syncing an updated cluster template.
func TestApplyNodeScalingChanges_OmniScalingIsAttempted(t *testing.T) {
	t.Parallel()

	testState := newInMemStateForOmniTest()

	// Seed a TalosVersion so resolveOmniVersions succeeds
	tv := omnires.NewTalosVersion("1.11.2")
	tv.TypedSpec().Value.CompatibleKubernetesVersions = []string{"1.32.0"}
	require.NoError(t, testState.Create(context.Background(), tv))

	// Seed a ready ClusterStatus so WaitForClusterReady returns immediately
	cs := omnires.NewClusterStatus("test")
	cs.TypedSpec().Value.Phase = specs.ClusterStatusSpec_RUNNING
	cs.TypedSpec().Value.Ready = true
	require.NoError(t, testState.Create(context.Background(), cs))

	omniProv := omniprovider.NewProviderWithState(testState)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithOmniOptions(v1alpha1.OptionsOmni{
			MachineClass: "test-class",
		}).
		WithInfraProvider(omniProv)
	result := clusterupdate.NewEmptyUpdateResult()

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.ControlPlanes = 1
	oldSpec.Workers = 0

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.ControlPlanes = 3
	newSpec.Workers = 2

	err := provisioner.ApplyNodeScalingChangesForTest(
		context.Background(),
		"test",
		oldSpec,
		newSpec,
		result,
	)
	require.NoError(t, err)
	assert.Len(t, result.AppliedChanges, 2, "expected 1 CP + 1 Worker applied scaling change")
}

// TestApplyNodeScalingChanges_BelowMinimumControlPlanes verifies that scaling
// below 1 control-plane node returns ErrMinimumControlPlanes.
func TestApplyNodeScalingChanges_BelowMinimumControlPlanes(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)
	result := clusterupdate.NewEmptyUpdateResult()

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.ControlPlanes = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.ControlPlanes = 0

	err := provisioner.ApplyNodeScalingChangesForTest(
		context.Background(),
		"test",
		oldSpec,
		newSpec,
		result,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, talosprovisioner.ErrMinimumControlPlanes)
}

// TestDiffConfig_DetectsBaselineNodeCountsWhenAutoscalingEnabled verifies that DiffConfig
// detects in-place changes for controlPlanes and workers even when autoscaling is enabled.
func TestDiffConfig_DetectsBaselineNodeCountsWhenAutoscalingEnabled(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.ControlPlanes = 1
	oldSpec.Workers = 0

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.ControlPlanes = 5
	newSpec.Workers = 3
	newSpec.NodeAutoscaling = v1alpha1.NodeAutoscalingEnabled

	result, err := provisioner.DiffConfig(context.Background(), "test", oldSpec, newSpec)
	require.NoError(t, err)
	assert.NotEmpty(
		t,
		result.InPlaceChanges,
		"baseline node count diffs should be detected when autoscaling is enabled",
	)
	assert.Len(t, result.InPlaceChanges, 2, "expected controlPlanes + workers changes")
}

// TestDiffConfig_StillValidatesMinimumControlPlanesWhenAutoscalingEnabled verifies that
// the controlPlanes >= 1 guard is enforced even when autoscaling is enabled.
func TestDiffConfig_StillValidatesMinimumControlPlanesWhenAutoscalingEnabled(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.ControlPlanes = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.ControlPlanes = 0 // invalid
	newSpec.NodeAutoscaling = v1alpha1.NodeAutoscalingEnabled

	_, err := provisioner.DiffConfig(context.Background(), "test", oldSpec, newSpec)
	require.Error(t, err)
	assert.ErrorIs(t, err, talosprovisioner.ErrMinimumControlPlanes)
}

// TestSyncSecretsFromCluster_NilTalosConfigs verifies that needsSecretSync
// returns false when talosConfigs is nil (e.g., tests that don't load configs),
// preventing syncSecretsFromCluster from being called.
func TestSyncSecretsFromCluster_NilTalosConfigs(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field: "workers", Category: clusterupdate.ChangeCategoryInPlace,
	})

	assert.False(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{Workers: 0}, &v1alpha1.ClusterSpec{Workers: 1}, diff,
	))
}

// TestSyncSecretsFromCluster_OmniSkipped verifies that needsSecretSync
// returns false for Omni-managed clusters (Omni handles config independently).
func TestSyncSecretsFromCluster_OmniSkipped(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{}).
		WithLogWriter(io.Discard)

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field: "workers", Category: clusterupdate.ChangeCategoryInPlace,
	})

	assert.False(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{Workers: 0}, &v1alpha1.ClusterSpec{Workers: 1}, diff,
	))
}

// TestNeedsSecretSync_ScaleUp verifies that needsSecretSync returns true
// when the update involves scaling up nodes.
func TestNeedsSecretSync_ScaleUp(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(configs, nil).WithLogWriter(io.Discard)
	diff := clusterupdate.NewEmptyUpdateResult()

	// Workers scale-up
	assert.True(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 1},
		diff,
	))

	// Control-plane scale-up
	assert.True(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		&v1alpha1.ClusterSpec{ControlPlanes: 3, Workers: 0},
		diff,
	))
}

// TestNeedsSecretSync_NoChanges verifies that needsSecretSync returns false
// when there are no scaling or config changes.
func TestNeedsSecretSync_NoChanges(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(configs, nil).WithLogWriter(io.Discard)
	diff := clusterupdate.NewEmptyUpdateResult()

	assert.False(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		diff,
	))
}

// TestNeedsSecretSync_ScaleDown verifies that needsSecretSync returns false
// for a pure scale-down with no other config changes (removing nodes doesn't
// need PKI sync).
func TestNeedsSecretSync_ScaleDown(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(configs, nil).WithLogWriter(io.Discard)
	diff := clusterupdate.NewEmptyUpdateResult()

	assert.False(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 2},
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 1},
		diff,
	))
}

// TestEnsureAutoscalerSecretIfNeeded_NoopWhenNotHetzner verifies that
// ensureAutoscalerSecretIfNeeded is a no-op when hetznerOpts is nil
// (non-Hetzner provider).
func TestEnsureAutoscalerSecretIfNeeded_NoopWhenNotHetzner(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	err := provisioner.EnsureAutoscalerSecretIfNeededForTest(
		context.Background(),
		"test-cluster",
	)
	require.NoError(t, err)
}

// TestEnsureAutoscalerSecretIfNeeded_NoopWhenAutoscalerDisabled verifies that
// ensureAutoscalerSecretIfNeeded is a no-op when NodeAutoscalerEnabled is false.
func TestEnsureAutoscalerSecretIfNeeded_NoopWhenAutoscalerDisabled(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: false,
		}).
		WithLogWriter(io.Discard)

	err := provisioner.EnsureAutoscalerSecretIfNeededForTest(
		context.Background(),
		"test-cluster",
	)
	require.NoError(t, err)
}

// TestEnsureAutoscalerSecretIfNeeded_NoopWhenNilTalosConfigs verifies that
// ensureAutoscalerSecretIfNeeded is a no-op when talosConfigs is nil, even
// when autoscaler is enabled on Hetzner.
func TestEnsureAutoscalerSecretIfNeeded_NoopWhenNilTalosConfigs(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
		}).
		WithLogWriter(io.Discard)

	err := provisioner.EnsureAutoscalerSecretIfNeededForTest(
		context.Background(),
		"test-cluster",
	)
	require.NoError(t, err)
}

// TestEnsureAutoscalerSecretIfNeeded_NoopWhenNilBundle verifies that
// ensureAutoscalerSecretIfNeeded is a no-op when talosConfigs is non-nil but
// Bundle() returns nil, preventing a nil-dereference panic.
func TestEnsureAutoscalerSecretIfNeeded_NoopWhenNilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfigmanager.Configs{}
	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
		}).
		WithTalosConfigsForTest(configs).
		WithLogWriter(io.Discard)

	err := provisioner.EnsureAutoscalerSecretIfNeededForTest(
		context.Background(),
		"test-cluster",
	)
	require.NoError(t, err)
}

// TestNeedsSecretSync_AutoscalerEnabled verifies that needsSecretSync returns
// true when the node autoscaler is enabled on Hetzner, even without node count
// changes. The autoscaler config secret embeds a worker config that must use
// the running cluster's PKI.
func TestNeedsSecretSync_AutoscalerEnabled(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
		}).
		WithLogWriter(io.Discard)
	diff := clusterupdate.NewEmptyUpdateResult()

	// No node count changes, but autoscaler is enabled → sync needed.
	assert.True(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		diff,
	))
}

// TestNeedsSecretSync_AutoscalerDisabledNoSync verifies that needsSecretSync
// returns false when hetznerOpts is set but the autoscaler is disabled and
// there are no node count changes.
func TestNeedsSecretSync_AutoscalerDisabledNoSync(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: false,
		}).
		WithLogWriter(io.Discard)
	diff := clusterupdate.NewEmptyUpdateResult()

	assert.False(t, provisioner.NeedsSecretSyncForTest(
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		&v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 0},
		diff,
	))
}

// TestEnsureAutoscalerSecretIfNeeded_ErrorWhenHcloudTokenNotSet verifies that
// ensureAutoscalerSecretIfNeeded returns ErrHcloudTokenNotSet when the autoscaler
// is enabled, a valid config bundle exists, a schematic is configured, but the
// HCLOUD_TOKEN env var is unset.
// This test mutates environment variables and cannot run in parallel.
func TestEnsureAutoscalerSecretIfNeeded_ErrorWhenHcloudTokenNotSet(t *testing.T) {
	// Unset the token env var to trigger the error path.
	t.Setenv(v1alpha1.DefaultHetznerTokenEnvVar, "")

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
		}).
		WithTalosOptsForTest(&v1alpha1.OptionsTalos{
			SchematicID: "test-schematic-id",
		}).
		WithTalosConfigsForTest(configs).
		WithLogWriter(io.Discard)

	err = provisioner.EnsureAutoscalerSecretIfNeededForTest(
		context.Background(),
		"test-cluster",
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, talosprovisioner.ErrHcloudTokenNotSet)
}

// TestEnsureAutoscalerSecretIfNeeded_ErrorWhenNoSchematic verifies that
// ensureAutoscalerSecretIfNeeded returns ErrAutoscalerRequiresSchematic when
// the autoscaler is enabled but no schematic ID or extensions are configured.
func TestEnsureAutoscalerSecretIfNeeded_ErrorWhenNoSchematic(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
		}).
		WithTalosOptsForTest(&v1alpha1.OptionsTalos{}).
		WithTalosConfigsForTest(configs).
		WithLogWriter(io.Discard)

	err = provisioner.EnsureAutoscalerSecretIfNeededForTest(
		context.Background(),
		"test-cluster",
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, talosprovisioner.ErrAutoscalerRequiresSchematic)
}

//nolint:funlen // Table-driven test with multiple node topology scenarios is clearer as single function
func TestDetectHetznerServerTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		nodes          []provider.NodeInfo
		wantCPType     string
		wantWorkerType string
	}{
		{
			name:           "empty node list returns empty strings",
			nodes:          nil,
			wantCPType:     "",
			wantWorkerType: "",
		},
		{
			name: "single control-plane node",
			nodes: []provider.NodeInfo{
				{Name: "cp-0", Role: talosprovisioner.RoleControlPlane, ServerType: "cx22"},
			},
			wantCPType:     "cx22",
			wantWorkerType: "",
		},
		{
			name: "single worker node",
			nodes: []provider.NodeInfo{
				{Name: "worker-0", Role: talosprovisioner.RoleWorker, ServerType: "cx33"},
			},
			wantCPType:     "",
			wantWorkerType: "cx33",
		},
		{
			name: "mixed nodes returns first of each role",
			nodes: []provider.NodeInfo{
				{Name: "cp-0", Role: talosprovisioner.RoleControlPlane, ServerType: "cx22"},
				{Name: "cp-1", Role: talosprovisioner.RoleControlPlane, ServerType: "cx44"},
				{Name: "worker-0", Role: talosprovisioner.RoleWorker, ServerType: "cx33"},
				{Name: "worker-1", Role: talosprovisioner.RoleWorker, ServerType: "cx55"},
			},
			wantCPType:     "cx22",
			wantWorkerType: "cx33",
		},
		{
			name: "skips nodes with empty ServerType",
			nodes: []provider.NodeInfo{
				{Name: "cp-0", Role: talosprovisioner.RoleControlPlane, ServerType: ""},
				{Name: "cp-1", Role: talosprovisioner.RoleControlPlane, ServerType: "cx22"},
				{Name: "worker-0", Role: talosprovisioner.RoleWorker, ServerType: ""},
			},
			wantCPType:     "cx22",
			wantWorkerType: "",
		},
		{
			name: "unknown roles are ignored",
			nodes: []provider.NodeInfo{
				{Name: "unknown-0", Role: "unknown", ServerType: "cx11"},
				{Name: "cp-0", Role: talosprovisioner.RoleControlPlane, ServerType: "cx22"},
			},
			wantCPType:     "cx22",
			wantWorkerType: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cpType, workerType := talosprovisioner.DetectHetznerServerTypesForTest(testCase.nodes)

			assert.Equal(t, testCase.wantCPType, cpType)
			assert.Equal(t, testCase.wantWorkerType, workerType)
		})
	}
}

func TestMergePersistedState_MergesISOFromSavedState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clusterName := "merge-iso-test-" + t.Name()
	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	// Save state with a specific ISO value
	savedSpec := &v1alpha1.ClusterSpec{
		Talos: v1alpha1.OptionsTalos{
			ISO: 125127,
		},
	}
	require.NoError(t, state.SaveClusterSpec(clusterName, savedSpec))

	// Create a spec from DefaultCurrentSpec (ISO will be 0)
	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	assert.Equal(t, int64(0), spec.Talos.ISO, "default spec should have zero ISO")

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard)
	require.NoError(t, provisioner.MergePersistedStateForTest(spec, clusterName))

	assert.Equal(t, int64(125127), spec.Talos.ISO,
		"mergePersistedState should fill ISO from saved state")
}

func TestMergePersistedState_MergesLocalRegistryFromSavedState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clusterName := "merge-registry-test-" + t.Name()
	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	savedSpec := &v1alpha1.ClusterSpec{
		LocalRegistry: v1alpha1.LocalRegistry{
			Registry: "ghcr.io/myorg",
		},
	}
	require.NoError(t, state.SaveClusterSpec(clusterName, savedSpec))

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	assert.Empty(t, spec.LocalRegistry.Registry, "default spec should have empty registry")

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard)
	require.NoError(t, provisioner.MergePersistedStateForTest(spec, clusterName))

	assert.Equal(t, "ghcr.io/myorg", spec.LocalRegistry.Registry,
		"mergePersistedState should fill LocalRegistry from saved state")
}

func TestMergePersistedState_NoStateIsNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	originalISO := spec.Talos.ISO

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard)
	require.NoError(t, provisioner.MergePersistedStateForTest(spec, "nonexistent-cluster-"+t.Name()))

	assert.Equal(t, originalISO, spec.Talos.ISO,
		"mergePersistedState should be no-op when no state exists")
}

func TestMergePersistedState_ZeroISOInStateIsNotMerged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clusterName := "zero-iso-test-" + t.Name()
	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	// Save state with zero ISO (unknown/unset)
	savedSpec := &v1alpha1.ClusterSpec{
		Talos: v1alpha1.OptionsTalos{
			ISO: 0,
		},
	}
	require.NoError(t, state.SaveClusterSpec(clusterName, savedSpec))

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard)
	require.NoError(t, provisioner.MergePersistedStateForTest(spec, clusterName))

	assert.Equal(t, int64(0), spec.Talos.ISO,
		"zero ISO in saved state should not override spec")
}

func TestMergePersistedState_ResolvesClusterNameFromConfigs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Use a name that resolveClusterName will return from talosConfigs
	configName := "talos-resolved-name-" + t.Name()
	t.Cleanup(func() { _ = state.DeleteClusterState(configName) })

	savedSpec := &v1alpha1.ClusterSpec{
		Talos: v1alpha1.OptionsTalos{
			ISO: 999999,
		},
	}
	require.NoError(t, state.SaveClusterSpec(configName, savedSpec))

	// Create provisioner with talosConfigs that has the cluster name
	tmpDir := t.TempDir()
	configsDir := filepath.Join(tmpDir, "configs")
	require.NoError(t, os.MkdirAll(configsDir, 0o700))

	configs := &talosconfigmanager.Configs{}
	configs.Name = configName

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithLogWriter(io.Discard)
	// Pass empty clusterName — should resolve from talosConfigs
	require.NoError(t, provisioner.MergePersistedStateForTest(spec, ""))

	assert.Equal(t, int64(999999), spec.Talos.ISO,
		"should resolve cluster name from talosConfigs and merge ISO")
}

func TestMergePersistedState_ReturnsErrorForCorruptState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clusterName := "corrupt-state-test-" + t.Name()

	// Write corrupt JSON to the state file
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	stateDir := filepath.Join(homeDir, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "spec.json"), []byte("not-json"), 0o600))
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard)

	err = provisioner.MergePersistedStateForTest(spec, clusterName)
	assert.Error(t, err, "corrupt state should return an error")
	assert.Contains(t, err.Error(), "load persisted cluster state")
}
