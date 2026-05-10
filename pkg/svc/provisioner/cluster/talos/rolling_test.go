package talosprovisioner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
)

func TestRolling_SortNodesWorkersFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nodes    []talosprovisioner.NodeWithRoleForTest
		expected []talosprovisioner.NodeWithRoleForTest
	}{
		{
			name:     "empty list",
			nodes:    nil,
			expected: []talosprovisioner.NodeWithRoleForTest{},
		},
		{
			name: "workers before control-planes",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.5", "worker"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.5", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
			},
		},
		{
			name: "only control-planes",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
			},
		},
		{
			name: "only workers",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.6", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.6", "worker"),
			},
		},
		{
			name: "single node",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
			},
		},
		{
			name: "IPs sorted within groups",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.9", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.7", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.8", "worker"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.7", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.8", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.9", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := talosprovisioner.SortNodesWorkersFirstForTest(tt.nodes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
