package k3shetzner

import "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"

// The K3s × Hetzner provisioner reports the sentinels of the shared Hetzner create
// flow; these aliases keep the package's public API stable while the behaviour lives
// on the shared create flow.
var (
	// ErrClusterAlreadyExists is returned by [Provisioner.Create] when servers for
	// the target K3s cluster already exist; see [hetznerbase.ErrClusterAlreadyExists].
	ErrClusterAlreadyExists = hetznerbase.ErrClusterAlreadyExists

	// ErrHAControlPlaneNotImplemented is returned by [Provisioner.Create] for a K3s
	// topology with more than one control plane. A single control plane with agents
	// is brought up via the two-phase multi-node flow; see
	// [hetznerbase.ErrHAControlPlaneNotImplemented].
	ErrHAControlPlaneNotImplemented = hetznerbase.ErrHAControlPlaneNotImplemented
)
