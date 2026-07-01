package kubeadmhetzner

import (
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// HetznerInfra exposes the unexported hetznerInfra interface so external tests can
// inject a fake provider in place of the live Hetzner Cloud API.
type HetznerInfra = hetznerInfra

// NewProvisionerForTest constructs a Provisioner with an injected infrastructure
// seam, bypassing the live Hetzner provider construction NewProvisioner performs.
func NewProvisionerForTest(
	infra HetznerInfra,
	clusterName, kubernetesVersion string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
	logWriter io.Writer,
) *Provisioner {
	return &Provisioner{
		infra:             infra,
		opts:              opts,
		clusterName:       clusterName,
		kubernetesVersion: kubernetesVersion,
		controlPlanes:     controlPlanes,
		agents:            agents,
		logWriter:         logWriter,
	}
}

// BuildNodes exposes the provisioner's buildNodes helper to external tests.
func (p *Provisioner) BuildNodes(clusterName, token string) ([]NodeUserData, error) {
	return p.buildNodes(clusterName, token)
}

// GenerateNodeToken exposes generateNodeToken to external tests.
func GenerateNodeToken() (string, error) {
	return generateNodeToken()
}
