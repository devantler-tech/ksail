package kubeadmhetzner

import (
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// HetznerInfra exposes the shared Hetzner infrastructure seam so external tests can
// inject a fake provider in place of the live Hetzner Cloud API.
type HetznerInfra = hetznerbase.Infra

// HetznerServers exposes the shared server-creation seam so external tests can
// inject a fake server creator in place of the live Hetzner Cloud API.
type HetznerServers = hetznerbase.ServerCreator

// NewProvisionerForTest constructs a Provisioner with an injected infrastructure
// seam, bypassing the live Hetzner provider construction NewProvisioner performs.
func NewProvisionerForTest(
	infra HetznerInfra,
	clusterName, kubernetesVersion string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
	logWriter io.Writer,
) *Provisioner {
	provisioner := &Provisioner{
		Base: &hetznerbase.Base{
			Infra:         infra,
			Opts:          opts,
			ClusterName:   clusterName,
			ControlPlanes: controlPlanes,
			Agents:        agents,
			// A non-empty destination satisfies RunCreate's fail-fast guard; tests
			// exercising the live bring-up inject their own Servers seam and
			// destination on the embedded Base.
			KubeconfigPath: "test-kubeconfig",
			LogWriter:      logWriter,
		},
		kubernetesVersion: kubernetesVersion,
	}
	provisioner.Strategy = provisioner

	return provisioner
}

// BuildNodes exposes the provisioner's buildNodes helper to external tests.
func (p *Provisioner) BuildNodes(clusterName, token string) ([]NodeUserData, error) {
	return p.buildNodes(clusterName, token, nil, nil)
}

// ComposePlan exposes the composed bring-up plan to external tests so the full
// composition — bootstrap material, user_data threading, spec derivation — can
// be asserted without a live bring-up.
func (p *Provisioner) ComposePlan(
	clusterName, token string,
	infra hetznerbase.ResolvedInfra,
) (hetznerbase.BringUpPlan, error) {
	return hetznerbase.PlanComposer(
		p.Opts,
		remoteKubeconfigPath,
		p.ComposeNodes,
	)(
		clusterName,
		token,
		infra,
	)
}

// GenerateNodeToken exposes generateNodeToken to external tests.
func GenerateNodeToken() (string, error) {
	return generateNodeToken()
}
