package kindprovisioner

import (
	"errors"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

// Static errors for the kind provisioner package.
var (
	// ErrNoDinDAddressPool means the bounded RFC1918 policy has no safe candidate.
	ErrNoDinDAddressPool = errors.New("no nested Docker address pool excludes the known networks")
	// ErrUnresolvedDinDEndpoint refuses allocation without a resolved required address.
	ErrUnresolvedDinDEndpoint = errors.New("required nested Docker endpoint has no usable address")
	// ErrIncompatibleDinDNetwork refuses reuse; existing resources are never removed here.
	ErrIncompatibleDinDNetwork = errors.New(
		"existing nested Docker network conflicts with its allocation policy",
	)

	// ErrNoKindNodes is returned when no Kind nodes are found for a cluster.
	ErrNoKindNodes = errors.New("no Kind nodes found for cluster")

	// ErrExecFailed is re-exported from the registry package for backward compatibility.
	ErrExecFailed = registry.ErrExecFailed
)
