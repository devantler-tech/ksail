package talosprovisioner

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// nextNodeIndexFromNames scans a slice of node names that share a common prefix
// and returns the next available numeric suffix to use after that prefix.
// Each name is expected to have the form "<prefix><n>" where n is a positive
// integer; the function computes max(n)+1 over all matching names and returns it.
// If no matching names are found (or no parsable numeric suffix is present),
// the function returns 1, which corresponds to the first suffix. Callers that
// maintain separate 0-based indexes for internal data structures may still need to
// map between this suffix and their own indexing scheme.
func nextNodeIndexFromNames(names []string, prefix string) int {
	maxIndex := 1

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
