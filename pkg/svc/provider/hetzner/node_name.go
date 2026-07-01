package hetzner

import (
	"errors"
	"fmt"
)

// MaxNodeNameLength is the maximum length of a Hetzner server name. A server name
// doubles as the Kubernetes node name, so it must be a valid 63-character DNS-1123
// label. ValidateClusterName caps the cluster name at 63, but the composed
// "-<node-type>-<index>" suffix can push the full name past the limit.
const MaxNodeNameLength = 63

// ErrNodeNameTooLong indicates a composed Hetzner server name exceeds
// [MaxNodeNameLength].
var ErrNodeNameTooLong = errors.New("hetzner node name too long")

// NodeName composes the deterministic Hetzner server name for a cluster node from
// its cluster name, node type ([NodeTypeControlPlane] or [NodeTypeWorker]) and
// index, and validates it against the 63-character DNS-1123 label limit. The name
// encodes the same identity as [NodeLabels] (built from the same three values), so
// a server can be found by either its name or its labels.
func NodeName(clusterName, nodeType string, index int) (string, error) {
	name := fmt.Sprintf("%s-%s-%d", clusterName, nodeType, index)
	if len(name) > MaxNodeNameLength {
		return name, fmt.Errorf(
			"%w: %q is %d characters (max %d); shorten the cluster name",
			ErrNodeNameTooLong, name, len(name), MaxNodeNameLength,
		)
	}

	return name, nil
}
