package talosprovisioner

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Errors returned when a replacement target cannot be bound to immutable identities.
var (
	// ErrReplacementTargetNotFound is returned when an identity the target needs is absent.
	ErrReplacementTargetNotFound = errors.New("replacement target not found")
	// ErrReplacementTargetAmbiguous is returned when more than one resource matches the target.
	ErrReplacementTargetAmbiguous = errors.New("replacement target is ambiguous")
	// ErrReplacementTargetNotOwned is returned when the target server is not owned by the cluster.
	ErrReplacementTargetNotOwned = errors.New("replacement target is not owned by the cluster")
	// ErrReplacementTargetInvalid is returned when an observed identity is incomplete or
	// contradictory.
	ErrReplacementTargetInvalid = errors.New("replacement target identity is invalid")
	// ErrReplacementPlanStale is returned when a resolved target no longer matches fresh observations.
	ErrReplacementPlanStale = errors.New("replacement plan is stale")
)

// replacementObservation is one read-only snapshot of the facts a single-node replacement
// binds to. Servers are the cluster's Hetzner servers plus any server carrying the target's
// name, so an unowned server with the same name is visible rather than silently skipped.
type replacementObservation struct {
	ClusterName string
	NodeName    string
	Servers     []*hcloud.Server
	Nodes       []corev1.Node
	EtcdMembers []*machineapi.EtcdMember
}

// replacementTarget is a node bound to identities that a recreated or renamed resource
// cannot reuse: the Hetzner server ID, the Kubernetes Node UID and, for a control-plane
// node, the etcd member ID.
type replacementTarget struct {
	ServerID     int64
	ServerName   string
	Role         string
	NodeUID      types.UID
	EtcdMemberID uint64
}

// resolveReplacementTarget binds the named node to its immutable identities. It rejects
// any absent, ambiguous, unowned or contradictory identity instead of guessing, because
// a later step deletes the resource it resolves.
func resolveReplacementTarget(observation replacementObservation) (replacementTarget, error) {
	server, err := uniqueOwnedServer(observation)
	if err != nil {
		return replacementTarget{}, err
	}

	target := replacementTarget{
		ServerID:   server.ID,
		ServerName: server.Name,
		Role:       server.Labels[hetzner.LabelNodeType],
	}

	node, err := uniqueNodeForServer(observation.Nodes, server)
	if err != nil {
		return replacementTarget{}, err
	}

	target.NodeUID = node.UID

	memberID, err := etcdMemberForTarget(observation.EtcdMembers, target)
	if err != nil {
		return replacementTarget{}, err
	}

	target.EtcdMemberID = memberID

	return target, nil
}

// revalidateReplacementTarget re-resolves the target from fresh observations and rejects
// the plan when any bound identity changed since it was resolved.
func revalidateReplacementTarget(
	planned replacementTarget,
	observation replacementObservation,
) (replacementTarget, error) {
	current, err := resolveReplacementTarget(observation)
	if err != nil {
		return replacementTarget{}, err
	}

	if current != planned {
		return replacementTarget{}, fmt.Errorf(
			"%w: planned %+v, observed %+v",
			ErrReplacementPlanStale,
			planned,
			current,
		)
	}

	return current, nil
}

func uniqueOwnedServer(observation replacementObservation) (*hcloud.Server, error) {
	var matches []*hcloud.Server

	for _, server := range observation.Servers {
		if server != nil && server.Name == observation.NodeName {
			matches = append(matches, server)
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf(
			"%w: no Hetzner server named %q", ErrReplacementTargetNotFound, observation.NodeName,
		)
	case 1:
	default:
		return nil, fmt.Errorf(
			"%w: %d Hetzner servers named %q",
			ErrReplacementTargetAmbiguous,
			len(matches),
			observation.NodeName,
		)
	}

	server := matches[0]

	if server.Labels[hetzner.LabelOwned] != hetzner.LabelOwnedValue ||
		server.Labels[hetzner.LabelClusterName] != observation.ClusterName {
		return nil, fmt.Errorf(
			"%w: server %q (ID %d) for cluster %q",
			ErrReplacementTargetNotOwned,
			server.Name,
			server.ID,
			observation.ClusterName,
		)
	}

	if server.ID <= 0 {
		return nil, fmt.Errorf("%w: server %q has no ID", ErrReplacementTargetInvalid, server.Name)
	}

	role := server.Labels[hetzner.LabelNodeType]
	if role != hetzner.NodeTypeControlPlane && role != hetzner.NodeTypeWorker {
		return nil, fmt.Errorf(
			"%w: server %q has unknown role %q", ErrReplacementTargetInvalid, server.Name, role,
		)
	}

	return server, nil
}

func uniqueNodeForServer(nodes []corev1.Node, server *hcloud.Server) (*corev1.Node, error) {
	serverIP := ""
	if !server.PublicNet.IPv4.IsUnspecified() {
		serverIP = server.PublicNet.IPv4.IP.String()
	}

	var matches []*corev1.Node

	for index := range nodes {
		if nodeMatchesServer(&nodes[index], server.Name, serverIP) {
			matches = append(matches, &nodes[index])
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf(
			"%w: no Kubernetes Node for server %q", ErrReplacementTargetNotFound, server.Name,
		)
	case 1:
	default:
		return nil, fmt.Errorf(
			"%w: %d Kubernetes Nodes match server %q",
			ErrReplacementTargetAmbiguous,
			len(matches),
			server.Name,
		)
	}

	if matches[0].UID == "" {
		return nil, fmt.Errorf(
			"%w: Kubernetes Node %q has no UID", ErrReplacementTargetInvalid, matches[0].Name,
		)
	}

	return matches[0], nil
}

func etcdMemberForTarget(
	members []*machineapi.EtcdMember,
	target replacementTarget,
) (uint64, error) {
	var matches []*machineapi.EtcdMember

	for _, member := range members {
		if member != nil && member.GetHostname() == target.ServerName {
			matches = append(matches, member)
		}
	}

	if target.Role == hetzner.NodeTypeWorker {
		if len(matches) > 0 {
			return 0, fmt.Errorf(
				"%w: worker %q is an etcd member", ErrReplacementTargetInvalid, target.ServerName,
			)
		}

		return 0, nil
	}

	switch len(matches) {
	case 0:
		return 0, fmt.Errorf(
			"%w: no etcd member for control-plane node %q",
			ErrReplacementTargetNotFound,
			target.ServerName,
		)
	case 1:
	default:
		return 0, fmt.Errorf(
			"%w: %d etcd members named %q",
			ErrReplacementTargetAmbiguous,
			len(matches),
			target.ServerName,
		)
	}

	if matches[0].GetId() == 0 {
		return 0, fmt.Errorf(
			"%w: etcd member %q has no ID", ErrReplacementTargetInvalid, target.ServerName,
		)
	}

	return matches[0].GetId(), nil
}
