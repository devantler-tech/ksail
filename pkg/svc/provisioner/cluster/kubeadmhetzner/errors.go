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

// ErrInvalidAdminKubeconfig is returned by [Provisioner.ComposeJoiningNodes]
// when the kubeconfig retrieved from the initial control plane does not carry a
// usable CA certificate for kubeadm's pinned token discovery.
var ErrInvalidAdminKubeconfig = errors.New(
	"kubeadm × Hetzner: init control-plane admin kubeconfig is invalid",
)

var errInvalidCAPEM = errors.New("CA data is not a PEM certificate")
