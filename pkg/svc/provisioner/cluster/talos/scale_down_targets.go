package talosprovisioner

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ErrControlPlaneScaleDownUnsafe refuses an unproven destructive removal plan.
var ErrControlPlaneScaleDownUnsafe = errors.New("control-plane scale-down is not safe")

// controlPlaneScaleDownTargets returns the newest removable control planes in
// reverse execution order. Names can be reused and never establish node age.
// An assigned owned floating endpoint remains on its current control plane.
func controlPlaneScaleDownTargets(
	servers []*hcloud.Server,
	floatingIP *hcloud.FloatingIP,
	clusterName string,
	count int,
) ([]*hcloud.Server, error) {
	if count <= 0 {
		return nil, nil
	}

	if count >= len(servers) {
		return nil, fmt.Errorf(
			"%w: removal must retain a control plane",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	seenIDs, err := validateRemovalCandidates(servers, clusterName)
	if err != nil {
		return nil, err
	}

	retainedID, err := retainedRemovalEndpoint(floatingIP, clusterName, seenIDs)
	if err != nil {
		return nil, err
	}

	candidates := make([]*hcloud.Server, 0, len(servers))
	for _, server := range servers {
		if server.ID != retainedID {
			candidates = append(candidates, server)
		}
	}

	slices.SortFunc(candidates, func(a, b *hcloud.Server) int {
		if order := b.Created.Compare(a.Created); order != 0 {
			return order
		}

		return strings.Compare(a.Name, b.Name)
	})

	if count > len(candidates) {
		return nil, fmt.Errorf("%w: too few candidates after preserving the endpoint",
			ErrControlPlaneScaleDownUnsafe)
	}

	targets := slices.Clone(candidates[:count])
	slices.Reverse(targets)

	err = proveRemovableTargets(targets)
	if err != nil {
		return nil, err
	}

	return targets, nil
}

func proveRemovableTargets(targets []*hcloud.Server) error {
	for _, target := range targets {
		err := proveRemovableTarget(target)
		if err != nil {
			return err
		}
	}

	return nil
}

// proveRemovableTarget refuses a selected target whose deletion cannot complete cleanly.
// Storage attached to a retained server does not block removal; storage or delete protection
// on a target does, because either would stop the provider deletion only after the target's
// etcd membership is gone. Selection never substitutes an older server for a refused target.
func proveRemovableTarget(server *hcloud.Server) error {
	if len(server.Volumes) > 0 {
		return fmt.Errorf(
			"%w: provider target has attached storage volumes",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	if server.Protection.Delete {
		return fmt.Errorf(
			"%w: provider target has delete protection enabled",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}

// validateRemovalTarget applies the candidate identity checks and the target-only checks.
func validateRemovalTarget(server *hcloud.Server, clusterName string) error {
	err := validateRemovalCandidate(server, clusterName)
	if err != nil {
		return err
	}

	return proveRemovableTarget(server)
}

func validateRemovalCandidates(
	servers []*hcloud.Server,
	clusterName string,
) (map[int64]bool, error) {
	seenIDs := make(map[int64]bool, len(servers))
	seenNames := make(map[string]bool, len(servers))

	for _, server := range servers {
		err := validateRemovalCandidate(server, clusterName)
		if err != nil {
			return nil, err
		}

		if seenIDs[server.ID] || seenNames[server.Name] {
			return nil, fmt.Errorf("%w: duplicate server identity", ErrControlPlaneScaleDownUnsafe)
		}

		seenIDs[server.ID], seenNames[server.Name] = true, true
	}

	return seenIDs, nil
}

func validateRemovalCandidate(server *hcloud.Server, clusterName string) error {
	if server == nil || server.ID <= 0 || server.Name == "" || server.Created.IsZero() {
		return fmt.Errorf(
			"%w: incomplete server identity or creation time",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	err := validateOwnedServer(server, clusterName)
	if err != nil {
		return err
	}

	if role := server.Labels[hetzner.LabelNodeType]; role != hetzner.NodeTypeControlPlane &&
		role != RoleControlPlane {
		return fmt.Errorf("%w: candidate is not a control plane", ErrControlPlaneScaleDownUnsafe)
	}

	return nil
}

func retainedRemovalEndpoint(
	floating *hcloud.FloatingIP,
	clusterName string,
	serverIDs map[int64]bool,
) (int64, error) {
	if floating == nil {
		return 0, nil
	}

	err := validateRemovalEndpoint(floating, clusterName)
	if err != nil {
		return 0, err
	}

	if !serverIDs[floating.Server.ID] {
		return 0, fmt.Errorf(
			"%w: endpoint holder is not a unique control plane",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return floating.Server.ID, nil
}

func validateRemovalEndpoint(floating *hcloud.FloatingIP, clusterName string) error {
	if floating == nil || floating.Server == nil {
		return fmt.Errorf("%w: floating endpoint is unassigned", ErrControlPlaneScaleDownUnsafe)
	}

	if floating.Name != clusterName+hetzner.FloatingIPSuffix ||
		floating.Labels[hetzner.LabelOwned] != hetzner.LabelOwnedValue ||
		floating.Labels[hetzner.LabelClusterName] != clusterName {
		return fmt.Errorf(
			"%w: floating endpoint ownership is unproven",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}
