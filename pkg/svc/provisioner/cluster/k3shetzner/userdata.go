package k3shetzner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	k3sbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/k3s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// nodeTokenBytes is the entropy of a generated k3s node token. 32 bytes (256
// bits) matches k3s's own default token length.
const nodeTokenBytes = 32

// nodeUserData pairs a planned node with the cloud-init user_data that bootstraps
// it and the Hetzner labels that identify the server it runs on.
type nodeUserData struct {
	// index is the node's zero-based bootstrap position (the cluster-initialising
	// server is 0).
	index int
	// role is the node's k3s cluster role.
	role k3sbootstrap.Role
	// userData is the cloud-init document delivered as the server's user_data.
	userData string
	// labels is the Hetzner label set applied to the server.
	labels map[string]string
}

// buildNodeUserData composes the ordered per-node cloud-init user_data for the
// cluster's topology. It plans the nodes (k3sbootstrap.Plan), renders each node's
// native install command (k3sbootstrap.Render), wraps it in a cloud-init document
// (the transport), and attaches the Hetzner node labels. It is pure — it performs
// no I/O and reaches no network — so it is fully unit-testable; serverURL is the
// address joining nodes register against (empty for a single control-plane node
// with no agents), controlPlanes and agents are the node counts to plan (passed
// explicitly so the two-phase multi-node flow can compose the init node and the
// joining nodes separately), sshAuthorizedKeys are the public keys delivered into
// every node's authorized_keys (nil for none) so the post-provision SSH bootstrap
// seam can authenticate, and hostKeys is the pre-generated SSH host identity
// delivered via the cloud-init ssh_keys module (nil to let the node generate its
// own) so the bootstrap dial can pin the host key.
func (p *Provisioner) buildNodeUserData(
	clusterName, token, serverURL string,
	controlPlanes, agents int,
	sshAuthorizedKeys []string,
	hostKeys *cloudinitbootstrap.HostKeys,
) ([]nodeUserData, error) {
	nodes, err := k3sbootstrap.Plan(k3sbootstrap.PlanInput{
		Version:           p.version,
		Token:             token,
		ControlPlaneCount: controlPlanes,
		AgentCount:        agents,
		ServerURL:         serverURL,
	})
	if err != nil {
		return nil, fmt.Errorf("plan k3s nodes: %w", err)
	}

	result := make([]nodeUserData, 0, len(nodes))

	for _, node := range nodes {
		command, renderErr := k3sbootstrap.Render(node.Config)
		if renderErr != nil {
			return nil, fmt.Errorf("render k3s install for node %d: %w", node.Index, renderErr)
		}

		userData, transportErr := p.transport.UserData(
			[]string{command}, sshAuthorizedKeys, hostKeys,
		)
		if transportErr != nil {
			return nil, fmt.Errorf("build cloud-init for node %d: %w", node.Index, transportErr)
		}

		result = append(result, nodeUserData{
			index:    node.Index,
			role:     node.Config.Role,
			userData: userData,
			labels:   hetzner.NodeLabels(clusterName, nodeType(node.Config.Role), node.Index),
		})
	}

	return result, nil
}

// nodeType maps a k3s role to the Hetzner node-type label value: agents are
// workers, every server role is a control plane.
func nodeType(role k3sbootstrap.Role) string {
	if role == k3sbootstrap.RoleAgent {
		return hetzner.NodeTypeWorker
	}

	return hetzner.NodeTypeControlPlane
}

// generateNodeToken returns a fresh, cryptographically random k3s node token that
// every node in the cluster authenticates with.
func generateNodeToken() (string, error) {
	buffer := make([]byte, nodeTokenBytes)

	_, err := rand.Read(buffer)
	if err != nil {
		return "", fmt.Errorf("generate node token: %w", err)
	}

	return hex.EncodeToString(buffer), nil
}
