package k3shetzner

import (
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// HetznerInfra exposes the shared Hetzner infrastructure seam so external tests can
// inject a fake provider in place of the live Hetzner Cloud API.
type HetznerInfra = hetznerbase.Infra

// HetznerServers exposes the shared server-creation seam so external tests can
// inject a fake server creator in place of the live Hetzner Cloud API.
type HetznerServers = hetznerbase.ServerCreator

// TestNode is the exported view of a composed node used by external tests.
type TestNode struct {
	Index    int
	Role     string
	UserData string
	Labels   map[string]string
}

// NewProvisionerForTest constructs a Provisioner with injected dependencies,
// bypassing the live Hetzner provider construction NewProvisioner performs.
func NewProvisionerForTest(
	infra HetznerInfra,
	transport cloudinitbootstrap.UserDataProvider,
	clusterName, version string,
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
		transport: transport,
		version:   version,
	}
	provisioner.Strategy = provisioner

	return provisioner
}

// BuildNodeUserData exposes buildNodeUserData to external tests as exported data.
func (p *Provisioner) BuildNodeUserData(
	clusterName, token, serverURL string,
	sshAuthorizedKeys []string,
	hostKeys *cloudinitbootstrap.HostKeys,
) ([]TestNode, error) {
	nodes, err := p.buildNodeUserData(
		clusterName, token, serverURL,
		p.ControlPlanes, p.Agents,
		sshAuthorizedKeys, hostKeys,
	)
	if err != nil {
		return nil, err
	}

	out := make([]TestNode, len(nodes))
	for i, node := range nodes {
		out[i] = TestNode{
			Index:    node.index,
			Role:     string(node.role),
			UserData: node.userData,
			Labels:   node.labels,
		}
	}

	return out, nil
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
