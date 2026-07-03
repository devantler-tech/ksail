package kubeadmhetzner

import "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"

// The kubeadm × Hetzner provisioner reports the sentinels of the shared Hetzner
// create flow; these aliases keep the package's public API stable while the
// behaviour lives on [hetznerbase.Base.RunCreate].
var (
	// ErrClusterAlreadyExists is returned by [Provisioner.Create] when servers for
	// the target kubeadm cluster already exist; see [hetznerbase.ErrClusterAlreadyExists].
	ErrClusterAlreadyExists = hetznerbase.ErrClusterAlreadyExists

	// ErrMultiNodeNotImplemented is returned by [Provisioner.Create] for a kubeadm
	// topology with agents — kubeadm does not yet implement joining-node bring-up;
	// see [hetznerbase.ErrMultiNodeNotImplemented].
	ErrMultiNodeNotImplemented = hetznerbase.ErrMultiNodeNotImplemented

	// ErrHAControlPlaneNotImplemented is returned by [Provisioner.Create] for a
	// kubeadm topology with more than one control plane; see
	// [hetznerbase.ErrHAControlPlaneNotImplemented].
	ErrHAControlPlaneNotImplemented = hetznerbase.ErrHAControlPlaneNotImplemented
)
