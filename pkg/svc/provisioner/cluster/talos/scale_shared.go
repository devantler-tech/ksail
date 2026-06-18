package talosprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// availableNodeIndices returns the next `count` 1-based node-name suffixes to
// allocate for a "<prefix><n>" series (Talos control-plane/worker names are
// 1-based), reclaiming the lowest freed index first so a recreated node reuses a
// removed node's name rather than drifting to max+1 (#5312). It is a thin Talos
// adapter over clusterupdate.AvailableNodeIndices; see there for the full
// semantics. Callers that maintain 0-based internal indexes map accordingly.
func availableNodeIndices(names []string, prefix string, count int) []int {
	return clusterupdate.AvailableNodeIndices(names, prefix, 1, count)
}

// recordAppliedChange adds an applied change to the update result.
func recordAppliedChange(result *clusterupdate.UpdateResult, role, nodeName, action string) {
	field := "cluster.workers"
	if role == RoleControlPlane {
		field = "cluster.controlPlanes"
	}

	result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
		Field:    field,
		NewValue: nodeName,
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   action + " " + role + " node",
	})
}

// recordFailedChange adds a failed change to the update result.
func recordFailedChange(result *clusterupdate.UpdateResult, role, nodeName string, err error) {
	field := "cluster.workers"
	if role == RoleControlPlane {
		field = "cluster.controlPlanes"
	}

	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
		Field:  field,
		Reason: fmt.Sprintf("failed to manage %s node %s: %v", role, nodeName, err),
	})
}
