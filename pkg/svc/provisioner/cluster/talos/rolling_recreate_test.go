package talosprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestRolesFromRollingChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fields     []string
		wantCP     bool
		wantWorker bool
	}{
		{name: "none", fields: nil, wantCP: false, wantWorker: false},
		{
			name:   "control plane only",
			fields: []string{"provider.hetzner.controlPlaneServerType"},
			wantCP: true,
		},
		{
			name:       "worker only",
			fields:     []string{"provider.hetzner.workerServerType"},
			wantWorker: true,
		},
		{
			name: "both",
			fields: []string{
				"provider.hetzner.controlPlaneServerType",
				"provider.hetzner.workerServerType",
			},
			wantCP:     true,
			wantWorker: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			changes := make([]clusterupdate.Change, 0, len(testCase.fields))
			for _, field := range testCase.fields {
				changes = append(changes, clusterupdate.Change{Field: field})
			}

			gotCP, gotWorker := talosprovisioner.RolesFromRollingChangesForTest(changes)
			assert.Equal(t, testCase.wantCP, gotCP)
			assert.Equal(t, testCase.wantWorker, gotWorker)
		})
	}
}

func TestServersNeedingReplacement(t *testing.T) {
	t.Parallel()

	server := func(name, serverType string) *hcloud.Server {
		srv := &hcloud.Server{Name: name}
		if serverType != "" {
			srv.ServerType = &hcloud.ServerType{Name: serverType}
		}

		return srv
	}

	servers := []*hcloud.Server{
		server("cp-1", "cx23"),
		server("cp-2", "cpx41"),
		server("cp-3", ""),
		nil,
	}

	out := talosprovisioner.ServersNeedingReplacementForTest(servers, "cpx41")

	// cp-1 (wrong type), cp-3 (unknown type) need replacement; cp-2 matches; nil skipped.
	names := make([]string, 0, len(out))
	for _, srv := range out {
		names = append(names, srv.Name)
	}

	assert.ElementsMatch(t, []string{"cp-1", "cp-3"}, names)
}

func TestServersNeedingReplacement_CaseInsensitive(t *testing.T) {
	t.Parallel()

	servers := []*hcloud.Server{
		{Name: "cp-1", ServerType: &hcloud.ServerType{Name: "CPX41"}},
	}

	out := talosprovisioner.ServersNeedingReplacementForTest(servers, "cpx41")
	assert.Empty(t, out, "matching type (case-insensitive) should not need replacement")
}

func TestAppendServerTypeChange(t *testing.T) { //nolint:funlen // table-driven tests
	t.Parallel()

	tests := []struct {
		name         string
		role         string
		current      string
		desired      string
		category     clusterupdate.ChangeCategory
		wantRolling  int
		wantRecreate int
	}{
		{
			name:        "rolling control plane",
			role:        "control-plane",
			current:     "cx23",
			desired:     "cpx41",
			category:    clusterupdate.ChangeCategoryRollingRecreate,
			wantRolling: 1,
		},
		{
			name:         "recreate below quorum",
			role:         "control-plane",
			current:      "cx23",
			desired:      "cpx41",
			category:     clusterupdate.ChangeCategoryRecreateRequired,
			wantRecreate: 1,
		},
		{
			name:     "in-place worker no existing nodes is dropped",
			role:     "worker",
			current:  "cx23",
			desired:  "cpx41",
			category: clusterupdate.ChangeCategoryInPlace,
		},
		{
			name:     "no change when types match",
			role:     "worker",
			current:  "cx23",
			desired:  "cx23",
			category: clusterupdate.ChangeCategoryRollingRecreate,
		},
		{
			name:     "no change when current unknown",
			role:     "worker",
			current:  "",
			desired:  "cpx41",
			category: clusterupdate.ChangeCategoryRollingRecreate,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diff := clusterupdate.NewEmptyUpdateResult()
			talosprovisioner.AppendServerTypeChangeForTest(
				diff, testCase.role, testCase.current, testCase.desired, testCase.category,
			)

			assert.Len(t, diff.RollingRecreate, testCase.wantRolling)
			assert.Len(t, diff.RecreateRequired, testCase.wantRecreate)
		})
	}
}

func TestNodeMatchesServer(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{}
	node.Name = "ksail-control-plane-4"
	node.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "10.0.0.5"},
		{Type: corev1.NodeExternalIP, Address: "203.0.113.7"},
	}

	assert.True(
		t,
		talosprovisioner.NodeMatchesServerForTest(node, "ksail-control-plane-4", "1.2.3.4"),
		"should match by name",
	)
	assert.True(
		t,
		talosprovisioner.NodeMatchesServerForTest(node, "KSAIL-CONTROL-PLANE-4", "1.2.3.4"),
		"name match should be case-insensitive",
	)
	assert.True(t, talosprovisioner.NodeMatchesServerForTest(node, "other", "203.0.113.7"),
		"should match by external IP")
	assert.False(t, talosprovisioner.NodeMatchesServerForTest(node, "other", "9.9.9.9"),
		"should not match unrelated node")
}

func TestNodeIsReady(t *testing.T) {
	t.Parallel()

	ready := &corev1.Node{}
	ready.Status.Conditions = []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
	}
	assert.True(t, talosprovisioner.NodeIsReadyForTest(ready))

	notReady := &corev1.Node{}
	notReady.Status.Conditions = []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
	}
	assert.False(t, talosprovisioner.NodeIsReadyForTest(notReady))

	assert.False(t, talosprovisioner.NodeIsReadyForTest(&corev1.Node{}),
		"node with no conditions is not ready")
}
