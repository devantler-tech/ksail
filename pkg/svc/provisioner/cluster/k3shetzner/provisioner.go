package k3shetzner

import (
	"fmt"
	"io"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// Provisioner provisions a K3s cluster on Hetzner Cloud servers by composing the
// native k3s install renderer, the cloud-init delivery transport, and the Hetzner
// provider's server lifecycle. See the package documentation for the scope of the
// current increment.
type Provisioner struct {
	infra         hetznerInfra
	transport     cloudinitbootstrap.UserDataProvider
	opts          v1alpha1.OptionsHetzner
	clusterName   string
	version       string
	controlPlanes int
	agents        int
	logWriter     io.Writer
}

// NewProvisioner constructs a K3s × Hetzner provisioner. It builds the Hetzner
// provider from opts (resolving the API token from the configured environment
// variable), mirroring how the Talos × Hetzner factory constructs its provider.
// version is the k3s release the nodes install (INSTALL_K3S_VERSION form, e.g.
// "v1.36.1+k3s1"); controlPlanes and agents are the node counts; clusterName is
// the default cluster name used when an operation is called with an empty name.
func NewProvisioner(
	clusterName, version string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
) (*Provisioner, error) {
	provider, _, err := hetzner.NewProviderFromOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("create Hetzner provider: %w", err)
	}

	return &Provisioner{
		infra:         provider,
		transport:     cloudinitbootstrap.New(),
		opts:          opts,
		clusterName:   clusterName,
		version:       version,
		controlPlanes: controlPlanes,
		agents:        agents,
		logWriter:     os.Stdout,
	}, nil
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
