package kubeadmhetzner

import (
	"errors"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// The kubeadm × Hetzner provisioner reports the sentinels of the shared Hetzner
// create flow; these aliases keep the package's public API stable while the
// behaviour lives on [hetznerbase.Base.RunCreate].
var (
	// ErrClusterAlreadyExists is returned by [Provisioner.Create] when servers for
	// the target kubeadm cluster already exist; see [hetznerbase.ErrClusterAlreadyExists].
	ErrClusterAlreadyExists = hetznerbase.ErrClusterAlreadyExists

	// ErrHAControlPlaneNotImplemented is returned by [Provisioner.Create] for a
	// kubeadm topology with more than one control plane; see
	// [hetznerbase.ErrHAControlPlaneNotImplemented].
	ErrHAControlPlaneNotImplemented = hetznerbase.ErrHAControlPlaneNotImplemented
)

// ErrJoiningNodesComposedFirst is returned by
// [Provisioner.ComposeJoiningNodes] when it is called before
// [Provisioner.ComposeInitNode] has minted the cluster CA — the two-phase flow
// guarantees the init compose runs first, so hitting this means the composer
// was driven outside that flow.
var ErrJoiningNodesComposedFirst = errors.New(
	"kubeadm × Hetzner: joining nodes composed before the init control plane",
)
