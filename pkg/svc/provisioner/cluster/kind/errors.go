package kindprovisioner

import (
	"errors"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
)

// Static errors for the kind provisioner package.
var (
	// ErrNoKindNodes is returned when no Kind nodes are found for a cluster.
	ErrNoKindNodes = errors.New("no Kind nodes found for cluster")

	// ErrExecFailed is re-exported from the registry package for backward compatibility.
	ErrExecFailed = registry.ErrExecFailed
)
