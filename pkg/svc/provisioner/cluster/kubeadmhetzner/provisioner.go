package kubeadmhetzner

import (
	"fmt"
	"io"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// Provisioner provisions a Vanilla (kubeadm) cluster on Hetzner Cloud servers by
// composing the declarative kubeadm install (via [BuildNodeUserData]), the
// cloud-init delivery transport, and the Hetzner provider's server lifecycle. It is
// the kubeadm sibling of the k3s × Hetzner provisioner and shares its Hetzner
// infrastructure lifecycle; see the package documentation for the scope of the
// current increment.
//
// Unlike the k3s provisioner it needs no cloud-init transport field: the kubeadm
// composer ([BuildNodeUserData]) assembles the full #cloud-config document itself
// (apt sources, packages, write_files, commands) rather than wrapping a single
// install command, so the provisioner hands the composer the topology and consumes
// the finished per-node user_data.
type Provisioner struct {
	infra             hetznerInfra
	opts              v1alpha1.OptionsHetzner
	clusterName       string
	kubernetesVersion string
	controlPlanes     int
	agents            int
	logWriter         io.Writer
}

// NewProvisioner constructs a kubeadm × Hetzner provisioner. It builds the Hetzner
// provider from opts (resolving the API token from the configured environment
// variable), mirroring how the k3s × Hetzner factory constructs its provider.
// kubernetesVersion is the Kubernetes release the nodes install (e.g. "v1.31.0"),
// which selects the community package repository track; controlPlanes and agents
// are the node counts; clusterName is the default cluster name used when an
// operation is called with an empty name.
func NewProvisioner(
	clusterName, kubernetesVersion string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
) (*Provisioner, error) {
	// Intentional sibling of k3shetzner.NewProvisioner: both build the shared Hetzner
	// provider from options and initialise the same fields; a future dedup could
	// extract a shared base (see #5650).
	// jscpd:ignore-start
	provider, _, err := hetzner.NewProviderFromOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("create Hetzner provider: %w", err)
	}

	return &Provisioner{
		infra:             provider,
		opts:              opts,
		clusterName:       clusterName,
		kubernetesVersion: kubernetesVersion,
		controlPlanes:     controlPlanes,
		agents:            agents,
		logWriter:         os.Stdout,
	}, nil
	// jscpd:ignore-end
}

// resolveName returns name when non-empty, otherwise the provisioner's configured
// default cluster name, matching the Provisioner interface's "empty name means use
// config default" contract.
func (p *Provisioner) resolveName(name string) string {
	if name != "" {
		return name
	}

	return p.clusterName
}
