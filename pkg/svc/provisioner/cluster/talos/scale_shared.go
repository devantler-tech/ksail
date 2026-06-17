package talosprovisioner

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// availableNodeIndices scans node names that share a common prefix and returns
// the next `count` numeric suffixes to allocate, preferring the lowest free
// indices. Gaps left by removed nodes are reclaimed (lowest first) before the
// series is extended past its highest used index, so a recreated node reclaims a
// lost node's name instead of drifting to an ever-higher index (#5312). Given
// existing indices {1, 3} and count 2 it returns [2, 4]; with no matching names
// it returns [1, 2, ..., count].
//
// Each name is expected to have the form "<prefix><n>" where n is a positive
// integer; names without the prefix or without a parsable positive suffix are
// ignored. The returned suffixes are 1-based; callers that maintain separate
// 0-based indexes for internal data structures map between this suffix and their
// own indexing scheme.
func availableNodeIndices(names []string, prefix string, count int) []int {
	if count <= 0 {
		return []int{}
	}

	used, maxIndex := scanUsedNodeIndices(names, prefix)
	indices := make([]int, 0, count)

	// Reclaim the lowest freed indices first (gaps within 1..maxIndex).
	for index := 1; index <= maxIndex && len(indices) < count; index++ {
		if !used[index] {
			indices = append(indices, index)
		}
	}

	// Extend the series past the highest used index for any remaining nodes.
	for index := maxIndex + 1; len(indices) < count; index++ {
		indices = append(indices, index)
	}

	return indices
}

// scanUsedNodeIndices parses the 1-based numeric suffixes of names sharing the
// given prefix, returning the set of indices in use and the highest one seen
// (0 when no name matches). Names without the prefix or without a parsable
// positive suffix are ignored.
func scanUsedNodeIndices(names []string, prefix string) (map[int]bool, int) {
	used := make(map[int]bool, len(names))
	maxIndex := 0

	for _, name := range names {
		suffix, found := strings.CutPrefix(name, prefix)
		if !found {
			continue
		}

		index, err := strconv.Atoi(suffix)
		if err != nil || index < 1 {
			continue
		}

		used[index] = true
		if index > maxIndex {
			maxIndex = index
		}
	}

	return used, maxIndex
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
