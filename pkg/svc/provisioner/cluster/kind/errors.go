package kindprovisioner

import "errors"

// Static errors for the kind provisioner package.
var (
	// ErrNoKindNodes is returned when no Kind nodes are found for a cluster.
	ErrNoKindNodes = errors.New("no Kind nodes found for cluster")

	// ErrExecFailed is returned when a container exec command fails.
	ErrExecFailed = errors.New("exec failed")
)
