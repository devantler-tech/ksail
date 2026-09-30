package talosprovisioner

import (
	"errors"
	"net"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

const (
	replacedServerID int64  = 201
	replacedMemberID uint64 = 7009
	replacedNodeUID         = "node-uid-9"
)

var errCompletionAPIRefused = errors.New("connection refused")

func plannedControlPlane() replacementTarget {
	return replacementTarget{
		ServerID:     101,
		ServerName:   targetName,
		Role:         hetzner.NodeTypeControlPlane,
		NodeUID:      "node-uid-1",
		EtcdMemberID: 7001,
	}
}

func readyNode(node corev1.Node, providerID string) corev1.Node {
	node.Spec.ProviderID = providerID
	node.Status.Conditions = []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
	}

	return node
}

// completedControlPlane is the observation after prod-control-plane-1 was replaced: a new
// server under the same name and address, a new Node object and a new etcd member.
func completedControlPlane() replacementCompletionObservation {
	return replacementCompletionObservation{
		replacementObservation: replacementObservation{
			ClusterName: targetCluster,
			NodeName:    targetName,
			Servers: []*hcloud.Server{
				targetServer(replacedServerID, targetName),
				targetServer(102, "prod-control-plane-2"),
			},
			Nodes: []corev1.Node{
				readyNode(targetNode(targetName, replacedNodeUID, targetIP), "hcloud://201"),
				readyNode(
					targetNode("prod-control-plane-2", "node-uid-2", "203.0.113.11"),
					"hcloud://102",
				),
			},
			EtcdMembers: []*machineapi.EtcdMember{
				{Id: replacedMemberID, Hostname: targetName},
				{Id: 7002, Hostname: "prod-control-plane-2"},
			},
		},
	}
}

func plannedMembership() []uint64 { return []uint64{7001, 7002} }

func TestProveReplacementCompletedAcceptsANewControlPlane(t *testing.T) {
	t.Parallel()

	current, err := proveReplacementCompleted(
		plannedControlPlane(), plannedMembership(), completedControlPlane(),
	)
	require.NoError(t, err)
	require.Equal(t, replacementTarget{
		ServerID:     replacedServerID,
		ServerName:   targetName,
		Role:         hetzner.NodeTypeControlPlane,
		NodeUID:      replacedNodeUID,
		EtcdMemberID: replacedMemberID,
	}, current)
}

func TestProveReplacementCompletedAcceptsANewWorker(t *testing.T) {
	t.Parallel()

	planned := plannedControlPlane()
	planned.Role = hetzner.NodeTypeWorker
	planned.EtcdMemberID = 0

	observation := completedControlPlane()
	observation.Servers[0].Labels = hetzner.NodeLabels(targetCluster, hetzner.NodeTypeWorker, 1)
	observation.EtcdMembers = observation.EtcdMembers[1:]

	current, err := proveReplacementCompleted(planned, nil, observation)
	require.NoError(t, err)
	require.Equal(t, replacedServerID, current.ServerID)
}

// A worker has no etcd member to disagree, so the name is the only fact that ties the
// observation back to the planned server: a different healthy worker must not count.
func TestProveReplacementCompletedRefusesAWorkerUnderAnotherName(t *testing.T) {
	t.Parallel()

	planned := plannedControlPlane()
	planned.Role = hetzner.NodeTypeWorker
	planned.EtcdMemberID = 0

	workerLabels := hetzner.NodeLabels(targetCluster, hetzner.NodeTypeWorker, 1)
	observation := completedControlPlane()
	observation.NodeName = "prod-control-plane-2"
	observation.Servers[0].Labels = workerLabels
	observation.Servers[1].Labels = workerLabels
	observation.Servers[1].PublicNet.IPv4.IP = net.ParseIP("203.0.113.11")
	observation.EtcdMembers = nil

	_, err := proveReplacementCompleted(planned, nil, observation)
	require.ErrorIs(t, err, ErrReplacementIncomplete)
	require.ErrorContains(
		t,
		err,
		`server name changed from "prod-control-plane-1" to "prod-control-plane-2"`,
	)
}

//nolint:funlen // Table-driven test coverage is naturally long.
func TestProveReplacementCompletedRefusesAnIncompleteReplacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		membership []uint64
		mutate     func(*replacementCompletionObservation)
		want       string
	}{
		{
			name: "unreachable API",
			mutate: func(o *replacementCompletionObservation) {
				o.APIErr = errCompletionAPIRefused
			},
			want: "unreachable through the original kubeconfig",
		},
		{
			name: "original server still serves the name",
			mutate: func(o *replacementCompletionObservation) {
				o.Servers[0].ID = 101
				o.Nodes[0].Spec.ProviderID = "hcloud://101"
			},
			want: "still backed by the original server 101",
		},
		{
			name: "original and new server both carry the name",
			mutate: func(o *replacementCompletionObservation) {
				o.Servers = append(o.Servers,
					targetServer(101, targetName))
			},
			want: "ambiguous",
		},
		{
			name: "original Node object answers",
			mutate: func(o *replacementCompletionObservation) {
				o.Nodes[0].UID = "node-uid-1"
			},
			want: `Node UID "node-uid-1" is the original node's`,
		},
		{
			name: "provider ID still names the original server",
			mutate: func(o *replacementCompletionObservation) {
				o.Nodes[0].Spec.ProviderID = "hcloud://101"
			},
			want: `provider ID "hcloud://101", want "hcloud://201"`,
		},
		{
			name: "provider ID not yet set",
			mutate: func(o *replacementCompletionObservation) {
				o.Nodes[0].Spec.ProviderID = ""
			},
			want: `provider ID "", want "hcloud://201"`,
		},
		{
			name: "new node not Ready",
			mutate: func(o *replacementCompletionObservation) {
				o.Nodes[0].Status.Conditions[0].Status = corev1.ConditionFalse
			},
			want: "is not Ready",
		},
		{
			name: "role changed",
			mutate: func(o *replacementCompletionObservation) {
				o.Servers[0].Labels = hetzner.NodeLabels(targetCluster, hetzner.NodeTypeWorker, 1)
				o.EtcdMembers = o.EtcdMembers[1:]
			},
			want: "role changed",
		},
		{
			name: "original etcd member still answers for the name",
			mutate: func(o *replacementCompletionObservation) {
				o.EtcdMembers[0].Id = 7001
			},
			want: "etcd member 7001 is the original member",
		},
		{
			name: "removed member still in the membership",
			mutate: func(o *replacementCompletionObservation) {
				o.EtcdMembers = append(
					o.EtcdMembers,
					&machineapi.EtcdMember{Id: 7001, Hostname: "old"},
				)
			},
			want: "etcd membership is [7001 7002 7009], want [7002 7009]",
		},
		{
			name: "a survivor dropped out of the membership",
			mutate: func(o *replacementCompletionObservation) {
				o.EtcdMembers = o.EtcdMembers[:1]
			},
			want: "etcd membership is [7009], want [7002 7009]",
		},
		{
			name: "new member still a learner",
			mutate: func(o *replacementCompletionObservation) {
				o.EtcdMembers[0].IsLearner = true
			},
			want: "learner",
		},
		{
			name:       "new member was already a member",
			membership: []uint64{7001, 7002, replacedMemberID},
			mutate:     func(*replacementCompletionObservation) {},
			want:       "7009 was already a member",
		},
		{
			name:       "planned membership lacks the replaced member",
			membership: []uint64{7002},
			mutate:     func(*replacementCompletionObservation) {},
			want:       "does not contain the replaced member 7001",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			membership := test.membership
			if membership == nil {
				membership = plannedMembership()
			}

			observation := completedControlPlane()
			test.mutate(&observation)

			_, err := proveReplacementCompleted(plannedControlPlane(), membership, observation)
			require.ErrorIs(t, err, ErrReplacementIncomplete)
			require.ErrorContains(t, err, test.want)
		})
	}
}
