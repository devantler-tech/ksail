package talosprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
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

// TestApplyNodeScalingChanges_NilSpecs verifies that nil specs short-circuit scaling without error.
func TestApplyNodeScalingChanges_NilSpecs(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := clusterupdate.NewEmptyUpdateResult()

	err := p.ApplyNodeScalingChangesForTest(context.Background(), "test", nil, nil, result)
	require.NoError(t, err)
}

// TestApplyNodeScalingChanges_NoDelta verifies that equal specs produce a no-op.
func TestApplyNodeScalingChanges_NoDelta(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := clusterupdate.NewEmptyUpdateResult()

	spec := &v1alpha1.ClusterSpec{}
	spec.Talos.ControlPlanes = 3
	spec.Talos.Workers = 2

	err := p.ApplyNodeScalingChangesForTest(context.Background(), "test", spec, spec, result)
	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalChanges(), "no changes expected when deltas are zero")
}

// TestApplyNodeScalingChanges_OmniReturnsNotImplemented verifies that the Omni
// provider path returns ErrNotImplemented when node scaling is requested.
// This documents the known limitation that Omni manages node scaling externally.
// See: https://github.com/devantler-tech/ksail/issues/3675
func TestApplyNodeScalingChanges_OmniReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil).WithOmniOptions(v1alpha1.OptionsOmni{
		Endpoint: "https://example.omni.siderolabs.io",
	})
	result := clusterupdate.NewEmptyUpdateResult()

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.Talos.ControlPlanes = 1
	oldSpec.Talos.Workers = 0

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 3
	newSpec.Talos.Workers = 2

	err := p.ApplyNodeScalingChangesForTest(context.Background(), "test", oldSpec, newSpec, result)
	require.Error(t, err)
	assert.ErrorIs(t, err, talosprovisioner.ErrNotImplemented)
}

// TestApplyNodeScalingChanges_BelowMinimumControlPlanes verifies that scaling
// below 1 control-plane node returns ErrMinimumControlPlanes.
func TestApplyNodeScalingChanges_BelowMinimumControlPlanes(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := clusterupdate.NewEmptyUpdateResult()

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.Talos.ControlPlanes = 1

	newSpec := &v1alpha1.ClusterSpec{}
	newSpec.Talos.ControlPlanes = 0

	err := p.ApplyNodeScalingChangesForTest(context.Background(), "test", oldSpec, newSpec, result)
	require.Error(t, err)
	assert.ErrorIs(t, err, talosprovisioner.ErrMinimumControlPlanes)
}
