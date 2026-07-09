package k3shetzner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// connectorSecretPrefix names this distribution's Connector kubeconfig Secret
// ("k3s-hetzner-<name>-kubeconfig") in the shared hub namespace; "k3s" alone would
// collide with the k3k (K3s-on-Kubernetes) naming space.
//
//nolint:gosec // G101 false positive: this is a Secret NAME prefix, not a credential.
const connectorSecretPrefix = "k3s-hetzner"

// Provisioner provisions a K3s cluster on Hetzner Cloud servers by composing the
// native k3s install renderer, the cloud-init delivery transport, and the shared
// Hetzner infrastructure lifecycle ([hetznerbase.Base]). See the package
// documentation for the scope of the current increment.
type Provisioner struct {
	*hetznerbase.Base

	transport cloudinitbootstrap.UserDataProvider
	version   string
}

// NewProvisioner constructs a K3s × Hetzner provisioner. It builds the shared
// [hetznerbase.Base] (which constructs the Hetzner provider from opts, resolving the
// API token from the configured environment variable), mirroring how the Talos ×
// Hetzner factory constructs its provider. version is the k3s release the nodes
// install (INSTALL_K3S_VERSION form, e.g. "v1.36.1+k3s1"); controlPlanes and agents
// are the node counts; clusterName is the default cluster name used when an
// operation is called with an empty name; kubeconfigPath is the local kubeconfig
// file a successful bring-up merges the admin kubeconfig into.
func NewProvisioner(
	clusterName, kubeconfigPath, version string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
) (*Provisioner, error) {
	provisioner := &Provisioner{
		transport: cloudinitbootstrap.New(),
		version:   version,
	}

	base, err := hetznerbase.NewBase(clusterName, kubeconfigPath, controlPlanes, agents, opts)
	if err != nil {
		return nil, fmt.Errorf("create K3s × Hetzner base: %w", err)
	}

	provisioner.Base = base
	base.Wire(provisioner, connectorSecretPrefix)

	return provisioner, nil
}
