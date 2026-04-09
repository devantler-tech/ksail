package talosprovisioner_test

import (
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
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

func TestUpdateSkipsOmniNodeScaling(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{}).
		WithLogWriter(io.Discard)

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.Talos.ControlPlanes = 1
	oldSpec.Talos.Workers = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 2
	newSpec.Talos.Workers = 2

	_, err := provisioner.Update(
		context.Background(),
		"demo",
		oldSpec,
		newSpec,
		clusterupdate.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}
}

// TestUpdateSkipsOmniInPlaceConfigApply verifies the omniOpts guard in Update() prevents
// applyInPlaceConfigChanges from pushing Talos machine configs to Omni-managed nodes.
// Non-nil talosConfigs are used so the guard (not the pre-existing talosConfigs nil-check)
// is what prevents the call — without the guard this test would fail with a Talos API error.
func TestUpdateSkipsOmniInPlaceConfigApply(t *testing.T) {
	t.Parallel()

	talosConfigs, err := talosconfigmanager.NewDefaultConfigs()
	if err != nil {
		t.Fatalf("NewDefaultConfigs() error = %v", err)
	}

	provisioner := talosprovisioner.NewProvisioner(talosConfigs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{}).
		WithLogWriter(io.Discard)

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.Talos.ControlPlanes = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 2

	_, err = provisioner.Update(
		context.Background(),
		"demo",
		oldSpec,
		newSpec,
		clusterupdate.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}
}

// TestUpdateSkipsOmniVersionUpgrade verifies the omniOpts guard in applyTalosVersionUpgrade
// prevents Talos OS upgrade attempts on Omni-managed clusters.
// Without the guard, Update() would try to query node versions via the Talos API and fail.
func TestUpdateSkipsOmniVersionUpgrade(t *testing.T) {
	t.Parallel()

	talosConfigs, err := talosconfigmanager.NewDefaultConfigs()
	if err != nil {
		t.Fatalf("NewDefaultConfigs() error = %v", err)
	}

	provisioner := talosprovisioner.NewProvisioner(talosConfigs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{}).
		WithLogWriter(io.Discard)

	// Identical specs: only the version upgrade step runs (no scaling/config changes).
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

// TestApplyNodeScalingChanges_OmniScalingIsSkipped verifies that the Omni
// provider path silently skips node scaling without error.
// Omni manages node scaling externally, so KSail just logs and returns nil.
// This was changed in https://github.com/devantler-tech/ksail/pull/3689.
func TestApplyNodeScalingChanges_OmniScalingIsSkipped(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithOmniOptions(v1alpha1.OptionsOmni{
			Endpoint: "https://example.omni.siderolabs.io",
		})
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
	assert.Equal(
		t,
		0,
		result.TotalChanges(),
		"no changes expected: Omni manages scaling externally",
	)
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
