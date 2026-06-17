package clusterupdate

import (
	"strconv"
	"strings"
)

// AvailableNodeIndices returns the next `count` node-name indices to allocate for
// a "<prefix><n>" naming series, preferring the lowest free indices. Gaps left by
// removed nodes are reclaimed (lowest first) before the series extends past its
// highest used index, so a recreated node reclaims a removed node's name instead
// of drifting to an ever-higher index (#5312).
//
// base is the first index in the series: 1 for Talos control-plane/worker names
// (e.g. "<cluster>-control-plane-1"), 0 for k3d agent names (e.g.
// "k3d-<cluster>-agent-0"). Names without the prefix, or whose suffix is not an
// integer >= base, are ignored.
//
// With base 1, existing indices {1, 3} and count 2 it returns [2, 4]; with no
// matching names it returns [base, base+1, ..., base+count-1].
func AvailableNodeIndices(names []string, prefix string, base, count int) []int {
	if count <= 0 {
		return []int{}
	}

	used := usedNodeIndices(names, prefix, base)
	indices := make([]int, 0, count)

	// Walk the series from base and take each free index. Gaps left by removed
	// nodes are reclaimed first; once they run out, every higher index is free
	// (nothing past the max is in `used`), so the series simply extends.
	for index := base; len(indices) < count; index++ {
		if !used[index] {
			indices = append(indices, index)
		}
	}

	return indices
}

// usedNodeIndices returns the set of numeric suffixes (>= base) in use among names
// sharing the given prefix. Names without the prefix or without a parsable suffix
// >= base are ignored.
func usedNodeIndices(names []string, prefix string, base int) map[int]bool {
	used := make(map[int]bool, len(names))

	for _, name := range names {
		suffix, found := strings.CutPrefix(name, prefix)
		if !found {
			continue
		}

		index, err := strconv.Atoi(suffix)
		if err != nil || index < base {
			continue
		}

		used[index] = true
	}

	return used
}
