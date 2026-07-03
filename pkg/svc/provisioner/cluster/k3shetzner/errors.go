package k3shetzner

import "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"

// The K3s × Hetzner provisioner reports the sentinels of the shared Hetzner create
// flow; these aliases keep the package's public API stable while the behaviour lives
// on [hetznerbase.Base.RunCreate].
var (
	// ErrClusterAlreadyExists is returned by [Provisioner.Create] when servers for
	// the target K3s cluster already exist; see [hetznerbase.ErrClusterAlreadyExists].
	ErrClusterAlreadyExists = hetznerbase.ErrClusterAlreadyExists

	// ErrMultiNodeNotImplemented is returned by [Provisioner.Create] for a K3s
	// topology with joining nodes; see [hetznerbase.ErrMultiNodeNotImplemented].
	ErrMultiNodeNotImplemented = hetznerbase.ErrMultiNodeNotImplemented
)
