package talosprovisioner_test

import (
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	omniprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
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
	oldSpec.Talos.ControlPlanes = 1
	oldSpec.Talos.Workers = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 2
	newSpec.Talos.Workers = 2

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
	spec.Talos.ControlPlanes = 1

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
	spec.Talos.ControlPlanes = 1

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
	oldSpec.Talos.ControlPlanes = 3
	oldSpec.Talos.Workers = 2

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 3
	newSpec.Talos.Workers = 2

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
	oldSpec.Talos.ControlPlanes = 1
	oldSpec.Talos.Workers = 0

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 3
	newSpec.Talos.Workers = 2

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
	oldSpec.Talos.ControlPlanes = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 0

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
