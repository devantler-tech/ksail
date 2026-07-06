package kubeadmhetzner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// connectorSecretPrefix names this distribution's Connector kubeconfig Secret
// ("vanilla-hetzner-<name>-kubeconfig") in the shared hub namespace, using the
// distribution's API name (Vanilla) rather than the implementation (kubeadm).
//
//nolint:gosec // G101 false positive: this is a Secret NAME prefix, not a credential.
const connectorSecretPrefix = "vanilla-hetzner"

// Provisioner provisions a Vanilla (kubeadm) cluster on Hetzner Cloud servers by
// composing the declarative kubeadm install (via [BuildNodeUserData]), the
// cloud-init delivery transport, and the shared Hetzner infrastructure lifecycle
// ([hetznerbase.Base]). It is the kubeadm sibling of the k3s × Hetzner provisioner;
// see the package documentation for the scope of the current increment.
//
// Unlike the k3s provisioner it needs no cloud-init transport field: the kubeadm
// composer ([BuildNodeUserData]) assembles the full #cloud-config document itself
// (apt sources, packages, write_files, commands) rather than wrapping a single
// install command, so the provisioner hands the composer the topology and consumes
// the finished per-node user_data.
type Provisioner struct {
	*hetznerbase.Base

	kubernetesVersion string

	// clusterPKI is the pre-seeded shared cluster PKI minted by
	// [Provisioner.ComposeInitNode] and consumed by
	// [Provisioner.ComposeJoiningNodes] within one two-phase create — the only
	// state the multi-node flow threads between its two compose calls. Nil until
	// an init node has been composed; a provisioner instance drives at most one
	// create, matching the shared flow's ordering guarantee.
	clusterPKI *ClusterPKI
}

// NewProvisioner constructs a kubeadm × Hetzner provisioner. It builds the shared
// [hetznerbase.Base] (which constructs the Hetzner provider from opts, resolving the
// API token from the configured environment variable), mirroring how the k3s ×
// Hetzner factory constructs its provider. kubernetesVersion is the Kubernetes
// release the nodes install (e.g. "v1.31.0"), which selects the community package
// repository track; controlPlanes and agents are the node counts; clusterName is
// the default cluster name used when an operation is called with an empty name;
// kubeconfigPath is the local kubeconfig file a successful bring-up merges the
// admin kubeconfig into.
func NewProvisioner(
	clusterName, kubeconfigPath, kubernetesVersion string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
) (*Provisioner, error) {
	provisioner := &Provisioner{
		kubernetesVersion: kubernetesVersion,
	}

	base, err := hetznerbase.NewBase(clusterName, kubeconfigPath, controlPlanes, agents, opts)
	if err != nil {
		return nil, fmt.Errorf("create kubeadm × Hetzner base: %w", err)
	}

	provisioner.Base = base
	base.Wire(provisioner, connectorSecretPrefix)

	return provisioner, nil
}
