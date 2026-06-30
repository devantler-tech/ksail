package k3shetzner

import (
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
)

// HetznerInfra exposes the unexported hetznerInfra interface so external tests can
// inject a fake provider in place of the live Hetzner Cloud API.
type HetznerInfra = hetznerInfra

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
		infra:         infra,
		transport:     transport,
		opts:          opts,
		clusterName:   clusterName,
		version:       version,
		controlPlanes: controlPlanes,
		agents:        agents,
		logWriter:     logWriter,
	}
}

// BuildNodeUserData exposes buildNodeUserData to external tests as exported data.
func (p *Provisioner) BuildNodeUserData(
	clusterName, token, serverURL string,
) ([]TestNode, error) {
	nodes, err := p.buildNodeUserData(clusterName, token, serverURL)
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
