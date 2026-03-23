package talosprovisioner

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
)

// nextNodeIndexFromNames scans a slice of node names that share a common prefix
// and returns the next available index. Each name is expected to have the form
// "<prefix><n>" where n is a non-negative integer. If no matching names are
// found the function returns 0 (first node).
func nextNodeIndexFromNames(names []string, prefix string) int {
	maxIndex := 0

	for _, name := range names {
		idx, found := strings.CutPrefix(name, prefix)
		if !found {
			continue
		}

		n, err := strconv.Atoi(idx)
		if err == nil && n >= maxIndex {
			maxIndex = n + 1
		}
	}

	return maxIndex
}

// recordAppliedChange adds an applied change to the update result.
func recordAppliedChange(result *clusterupdate.UpdateResult, role, nodeName, action string) {
	field := "talos.workers"
	if role == RoleControlPlane {
		field = "talos.controlPlanes"
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
	field := "talos.workers"
	if role == RoleControlPlane {
		field = "talos.controlPlanes"
	}

	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
		Field:  field,
		Reason: fmt.Sprintf("failed to manage %s node %s: %v", role, nodeName, err),
	})
}
