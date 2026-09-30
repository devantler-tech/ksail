package talosprovisioner

import (
	"errors"
	"fmt"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	corev1 "k8s.io/api/core/v1"
)

// ErrReplacementIncomplete is returned when fresh observations do not prove that a
// single-node replacement produced a genuinely new, healthy node.
var ErrReplacementIncomplete = errors.New("replacement is not complete")

// hetznerProviderIDPrefix is the scheme the Hetzner cloud controller manager writes into
// a Node's spec.providerID, followed by the server ID.
const hetznerProviderIDPrefix = "hcloud://"

// replacementCompletionObservation is one read-only snapshot taken after a replacement.
// APIErr is the result of probing the cluster API through the kubeconfig the operator
// started with, so a replacement that broke the stable endpoint cannot count as complete.
type replacementCompletionObservation struct {
	replacementObservation

	APIErr error
}

// proveReplacementCompleted succeeds only when the replaced name is now backed by a new
// owned server, a new Ready Kubernetes Node bound to that server by provider ID, and — for
// a control plane — an etcd membership that is exactly the planned one with the removed
// member swapped for a new voting member. Name and address are reused by a replacement, so
// none of these facts may be inferred from them alone.
func proveReplacementCompleted(
	planned replacementTarget,
	plannedMembership []uint64,
	observation replacementCompletionObservation,
) (replacementTarget, error) {
	if observation.APIErr != nil {
		return replacementTarget{}, fmt.Errorf(
			"%w: cluster API is unreachable through the original kubeconfig: %w",
			ErrReplacementIncomplete,
			observation.APIErr,
		)
	}

	current, err := resolveReplacementTarget(observation.replacementObservation)
	if err != nil {
		return replacementTarget{}, fmt.Errorf("%w: %w", ErrReplacementIncomplete, err)
	}

	err = newIdentities(planned, current)
	if err != nil {
		return replacementTarget{}, err
	}

	err = newNodeReady(observation.Nodes, current)
	if err != nil {
		return replacementTarget{}, err
	}

	if current.Role == hetzner.NodeTypeControlPlane {
		err = swappedMembership(planned, current, plannedMembership, observation)
		if err != nil {
			return replacementTarget{}, err
		}
	}

	return current, nil
}

func newIdentities(planned, current replacementTarget) error {
	switch {
	case current.Role != planned.Role:
		return fmt.Errorf("%w: role changed from %q to %q",
			ErrReplacementIncomplete, planned.Role, current.Role)
	case current.ServerID == planned.ServerID:
		return fmt.Errorf("%w: %q is still backed by the original server %d",
			ErrReplacementIncomplete, current.ServerName, current.ServerID)
	case current.NodeUID == planned.NodeUID:
		return fmt.Errorf("%w: Kubernetes Node UID %q is the original node's",
			ErrReplacementIncomplete, current.NodeUID)
	case current.Role == hetzner.NodeTypeControlPlane &&
		current.EtcdMemberID == planned.EtcdMemberID:
		return fmt.Errorf("%w: etcd member %d is the original member",
			ErrReplacementIncomplete, current.EtcdMemberID)
	}

	return nil
}

func newNodeReady(nodes []corev1.Node, current replacementTarget) error {
	index := slices.IndexFunc(nodes, func(node corev1.Node) bool {
		return node.UID == current.NodeUID
	})
	if index < 0 {
		return fmt.Errorf("%w: no Kubernetes Node with UID %q",
			ErrReplacementIncomplete, current.NodeUID)
	}

	node := &nodes[index]

	want := fmt.Sprintf("%s%d", hetznerProviderIDPrefix, current.ServerID)
	if node.Spec.ProviderID != want {
		return fmt.Errorf("%w: Kubernetes Node %q has provider ID %q, want %q",
			ErrReplacementIncomplete, node.Name, node.Spec.ProviderID, want)
	}

	if !nodeIsReady(node) {
		return fmt.Errorf(
			"%w: Kubernetes Node %q is not Ready",
			ErrReplacementIncomplete,
			node.Name,
		)
	}

	return nil
}

func swappedMembership(
	planned, current replacementTarget,
	plannedMembership []uint64,
	observation replacementCompletionObservation,
) error {
	if !slices.Contains(plannedMembership, planned.EtcdMemberID) {
		return fmt.Errorf("%w: planned membership %v does not contain the replaced member %d",
			ErrReplacementIncomplete, plannedMembership, planned.EtcdMemberID)
	}

	if slices.Contains(plannedMembership, current.EtcdMemberID) {
		return fmt.Errorf("%w: etcd member %d was already a member before the replacement",
			ErrReplacementIncomplete, current.EtcdMemberID)
	}

	observed, err := votingMemberIDs(observation.EtcdMembers)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReplacementIncomplete, err)
	}

	expected := make([]uint64, 0, len(plannedMembership))
	for _, id := range plannedMembership {
		if id != planned.EtcdMemberID {
			expected = append(expected, id)
		}
	}

	expected = append(expected, current.EtcdMemberID)
	slices.Sort(expected)

	if !slices.Equal(observed, expected) {
		return fmt.Errorf("%w: etcd membership is %v, want %v",
			ErrReplacementIncomplete, observed, expected)
	}

	return nil
}
