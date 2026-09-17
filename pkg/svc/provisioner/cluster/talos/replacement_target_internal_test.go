package talosprovisioner

import (
	"net"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	targetCluster = "prod"
	targetName    = "prod-control-plane-1"
	targetIP      = "203.0.113.10"
)

func targetServer(id int64, name, cluster, role string) *hcloud.Server {
	return &hcloud.Server{
		ID:     id,
		Name:   name,
		Labels: hetzner.NodeLabels(cluster, role, 1),
		PublicNet: hcloud.ServerPublicNet{
			IPv4: hcloud.ServerPublicNetIPv4{IP: net.ParseIP(targetIP)},
		},
	}
}

func targetNode(name string, uid types.UID, address string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: uid},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: address}},
		},
	}
}

func controlPlaneObservation() replacementObservation {
	return replacementObservation{
		ClusterName: targetCluster,
		NodeName:    targetName,
		Servers: []*hcloud.Server{
			targetServer(101, targetName, targetCluster, hetzner.NodeTypeControlPlane),
			targetServer(102, "prod-control-plane-2", targetCluster, hetzner.NodeTypeControlPlane),
		},
		Nodes: []corev1.Node{
			targetNode(targetName, "node-uid-1", targetIP),
			targetNode("prod-control-plane-2", "node-uid-2", "203.0.113.11"),
		},
		EtcdMembers: []*machineapi.EtcdMember{
			{Id: 7001, Hostname: targetName},
			{Id: 7002, Hostname: "prod-control-plane-2"},
		},
	}
}

func workerObservation() replacementObservation {
	observation := controlPlaneObservation()
	observation.Servers[0].Labels = hetzner.NodeLabels(targetCluster, hetzner.NodeTypeWorker, 1)
	observation.EtcdMembers = observation.EtcdMembers[1:]

	return observation
}

func TestResolveReplacementTargetBindsImmutableIdentities(t *testing.T) {
	t.Parallel()

	target, err := resolveReplacementTarget(controlPlaneObservation())
	require.NoError(t, err)
	require.Equal(t, replacementTarget{
		ServerID:     101,
		ServerName:   targetName,
		Role:         hetzner.NodeTypeControlPlane,
		NodeUID:      "node-uid-1",
		EtcdMemberID: 7001,
	}, target)

	worker, err := resolveReplacementTarget(workerObservation())
	require.NoError(t, err)
	require.Equal(t, hetzner.NodeTypeWorker, worker.Role)
	require.Zero(t, worker.EtcdMemberID)
}

// TestResolveReplacementTargetMatchesNodeByAddress keeps a renamed Kubernetes Node bound
// through the server's public address, the same match the rolling replacement uses.
func TestResolveReplacementTargetMatchesNodeByAddress(t *testing.T) {
	t.Parallel()

	observation := controlPlaneObservation()
	observation.Nodes[0].Name = "renamed"

	target, err := resolveReplacementTarget(observation)
	require.NoError(t, err)
	require.Equal(t, types.UID("node-uid-1"), target.NodeUID)
}

//nolint:funlen // One table enumerates every rejection path the resolver guards.
func TestResolveReplacementTargetRejectsUnprovableIdentities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*replacementObservation)
		wantErr error
	}{
		{
			name:    "no server with the name",
			mutate:  func(o *replacementObservation) { o.NodeName = "missing" },
			wantErr: ErrReplacementTargetNotFound,
		},
		{
			name: "two servers with the name",
			mutate: func(o *replacementObservation) {
				o.Servers = append(o.Servers,
					targetServer(103, targetName, targetCluster, hetzner.NodeTypeControlPlane))
			},
			wantErr: ErrReplacementTargetAmbiguous,
		},
		{
			name: "server without the owned label",
			mutate: func(o *replacementObservation) {
				delete(o.Servers[0].Labels, hetzner.LabelOwned)
			},
			wantErr: ErrReplacementTargetNotOwned,
		},
		{
			name: "server owned by another cluster",
			mutate: func(o *replacementObservation) {
				o.Servers[0].Labels[hetzner.LabelClusterName] = "staging"
			},
			wantErr: ErrReplacementTargetNotOwned,
		},
		{
			name:    "server without an ID",
			mutate:  func(o *replacementObservation) { o.Servers[0].ID = 0 },
			wantErr: ErrReplacementTargetInvalid,
		},
		{
			name: "server with an unknown role",
			mutate: func(o *replacementObservation) {
				o.Servers[0].Labels[hetzner.LabelNodeType] = "gateway"
			},
			wantErr: ErrReplacementTargetInvalid,
		},
		{
			name:    "no Kubernetes Node",
			mutate:  func(o *replacementObservation) { o.Nodes = o.Nodes[1:] },
			wantErr: ErrReplacementTargetNotFound,
		},
		{
			name: "two Kubernetes Nodes for the server",
			mutate: func(o *replacementObservation) {
				o.Nodes = append(o.Nodes, targetNode("reused-address", "node-uid-3", targetIP))
			},
			wantErr: ErrReplacementTargetAmbiguous,
		},
		{
			name:    "Kubernetes Node without a UID",
			mutate:  func(o *replacementObservation) { o.Nodes[0].UID = "" },
			wantErr: ErrReplacementTargetInvalid,
		},
		{
			name:    "control-plane node without an etcd member",
			mutate:  func(o *replacementObservation) { o.EtcdMembers = o.EtcdMembers[1:] },
			wantErr: ErrReplacementTargetNotFound,
		},
		{
			name: "two etcd members for the node",
			mutate: func(o *replacementObservation) {
				o.EtcdMembers = append(o.EtcdMembers,
					&machineapi.EtcdMember{Id: 7003, Hostname: targetName})
			},
			wantErr: ErrReplacementTargetAmbiguous,
		},
		{
			name:    "etcd member without an ID",
			mutate:  func(o *replacementObservation) { o.EtcdMembers[0].Id = 0 },
			wantErr: ErrReplacementTargetInvalid,
		},
		{
			name: "worker that is an etcd member",
			mutate: func(o *replacementObservation) {
				o.Servers[0].Labels[hetzner.LabelNodeType] = hetzner.NodeTypeWorker
			},
			wantErr: ErrReplacementTargetInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observation := controlPlaneObservation()
			test.mutate(&observation)

			_, err := resolveReplacementTarget(observation)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestRevalidateReplacementTargetRejectsChangedIdentities(t *testing.T) {
	t.Parallel()

	planned, err := resolveReplacementTarget(controlPlaneObservation())
	require.NoError(t, err)

	current, err := revalidateReplacementTarget(planned, controlPlaneObservation())
	require.NoError(t, err)
	require.Equal(t, planned, current)

	tests := []struct {
		name   string
		mutate func(*replacementObservation)
	}{
		{
			name:   "server recreated",
			mutate: func(o *replacementObservation) { o.Servers[0].ID = 201 },
		},
		{
			name:   "Node re-registered",
			mutate: func(o *replacementObservation) { o.Nodes[0].UID = "node-uid-9" },
		},
		{
			name:   "etcd member rejoined",
			mutate: func(o *replacementObservation) { o.EtcdMembers[0].Id = 7009 },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observation := controlPlaneObservation()
			test.mutate(&observation)

			_, err := revalidateReplacementTarget(planned, observation)
			require.ErrorIs(t, err, ErrReplacementPlanStale)
		})
	}

	t.Run("role changed", func(t *testing.T) {
		t.Parallel()

		_, err := revalidateReplacementTarget(planned, workerObservation())
		require.ErrorIs(t, err, ErrReplacementPlanStale)
	})

	t.Run("target now unprovable", func(t *testing.T) {
		t.Parallel()

		observation := controlPlaneObservation()
		observation.Nodes = nil

		_, err := revalidateReplacementTarget(planned, observation)
		require.ErrorIs(t, err, ErrReplacementTargetNotFound)
	})
}
