package talosprovisioner

import (
	"testing"
)

func TestCountNodeRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		nodes      []nodeWithRole
		wantCP     int
		wantWorker int
	}{
		{
			name:       "empty node list defaults to 1 CP",
			nodes:      nil,
			wantCP:     1,
			wantWorker: 0,
		},
		{
			name: "single control-plane node",
			nodes: []nodeWithRole{
				{IP: "10.0.0.2", Role: RoleControlPlane},
			},
			wantCP:     1,
			wantWorker: 0,
		},
		{
			name: "3 control-planes and 2 workers",
			nodes: []nodeWithRole{
				{IP: "10.0.0.2", Role: RoleControlPlane},
				{IP: "10.0.0.3", Role: RoleControlPlane},
				{IP: "10.0.0.4", Role: RoleControlPlane},
				{IP: "10.0.0.5", Role: RoleWorker},
				{IP: "10.0.0.6", Role: RoleWorker},
			},
			wantCP:     3,
			wantWorker: 2,
		},
		{
			name: "only workers defaults CP to 1",
			nodes: []nodeWithRole{
				{IP: "10.0.0.5", Role: RoleWorker},
			},
			wantCP:     1,
			wantWorker: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cp, worker := countNodeRoles(testCase.nodes)

			if cp != testCase.wantCP {
				t.Errorf("countNodeRoles() CP = %d, want %d", cp, testCase.wantCP)
			}

			if worker != testCase.wantWorker {
				t.Errorf("countNodeRoles() worker = %d, want %d", worker, testCase.wantWorker)
			}
		})
	}
}
