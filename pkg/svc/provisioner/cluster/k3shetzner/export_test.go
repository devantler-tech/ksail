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
		transport: transport,
		version:   version,
	}
}

// BuildNodeUserData exposes buildNodeUserData to external tests as exported data.
func (p *Provisioner) BuildNodeUserData(
	clusterName, token, serverURL string,
	sshAuthorizedKeys []string,
) ([]TestNode, error) {
	nodes, err := p.buildNodeUserData(clusterName, token, serverURL, sshAuthorizedKeys)
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

// GenerateNodeToken exposes generateNodeToken to external tests.
func GenerateNodeToken() (string, error) {
	return generateNodeToken()
}
