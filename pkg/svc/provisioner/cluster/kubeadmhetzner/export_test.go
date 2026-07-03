package kubeadmhetzner

import (
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// HetznerInfra exposes the shared Hetzner infrastructure seam so external tests can
// inject a fake provider in place of the live Hetzner Cloud API.
type HetznerInfra = hetznerbase.Infra

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
		Base: &hetznerbase.Base{
			Infra:         infra,
			Opts:          opts,
			ClusterName:   clusterName,
			ControlPlanes: controlPlanes,
			Agents:        agents,
			// A non-empty destination satisfies RunCreate's fail-fast guard; the
			// gated composePlan stops before anything is persisted there.
			KubeconfigPath: "test-kubeconfig",
			LogWriter:      logWriter,
		},
		kubernetesVersion: kubernetesVersion,
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
