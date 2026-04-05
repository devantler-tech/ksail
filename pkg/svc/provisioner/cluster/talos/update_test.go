package talosprovisioner_test

import (
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
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
