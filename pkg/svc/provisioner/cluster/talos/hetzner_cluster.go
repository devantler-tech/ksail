package talosprovisioner

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/provision"
)

// HetznerProvisionerName is the provisioner name for Hetzner clusters.
const HetznerProvisionerName = "hetzner"

// ErrNoStatePath is returned when state path is not available for Hetzner clusters.
var ErrNoStatePath = errors.New("state path not used for Hetzner provisioner")

// HetznerClusterResult implements provision.Cluster for Hetzner Cloud clusters.
// This allows integration with upstream Talos SDK patterns like access.NewAdapter()
// and check.Wait() for cluster health checks.
type HetznerClusterResult struct {
	clusterInfo provision.ClusterInfo
	statePath   string
}

// Ensure HetznerClusterResult implements provision.Cluster.
var _ provision.Cluster = (*HetznerClusterResult)(nil)

// NewHetznerClusterResult creates a new HetznerClusterResult from Hetzner servers.
func NewHetznerClusterResult(
	clusterName string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
	kubernetesEndpoint string,
) (*HetznerClusterResult, error) {
	nodes := make([]provision.NodeInfo, 0, len(controlPlaneServers)+len(workerServers))

	// Add control plane nodes
	for _, server := range controlPlaneServers {
		nodeInfo, err := hetznerServerToNodeInfo(server, machine.TypeControlPlane)
		if err != nil {
			return nil, err
		}

		nodes = append(nodes, *nodeInfo)
	}

	// Add worker nodes
	for _, server := range workerServers {
		nodeInfo, err := hetznerServerToNodeInfo(server, machine.TypeWorker)
		if err != nil {
			return nil, err
		}

		nodes = append(nodes, *nodeInfo)
	}

	return &HetznerClusterResult{
		clusterInfo: provision.ClusterInfo{
			ClusterName:        clusterName,
			Nodes:              nodes,
			KubernetesEndpoint: kubernetesEndpoint,
		},
	}, nil
}

// Provisioner returns the name of the provisioner.
func (r *HetznerClusterResult) Provisioner() string {
	return HetznerProvisionerName
}

// StatePath returns the path to the state directory.
// For Hetzner clusters, we don't use local state files like Docker/QEMU.
func (r *HetznerClusterResult) StatePath() (string, error) {
	if r.statePath == "" {
		return "", ErrNoStatePath
	}

	return r.statePath, nil
}

// Info returns the cluster information.
func (r *HetznerClusterResult) Info() provision.ClusterInfo {
	return r.clusterInfo
}

// SetStatePath sets the state path for the cluster.
func (r *HetznerClusterResult) SetStatePath(path string) {
	r.statePath = path
}

// hetznerServerToNodeInfo converts a Hetzner server to a provision.NodeInfo.
func hetznerServerToNodeInfo(
	server *hcloud.Server,
	nodeType machine.Type,
) (*provision.NodeInfo, error) {
	ips := make([]netip.Addr, 0, initialIPCapacity)

	// Add public IPv4
	if server.PublicNet.IPv4.IP != nil {
		ip, err := netip.ParseAddr(server.PublicNet.IPv4.IP.String())
		if err != nil {
			return nil, fmt.Errorf("failed to parse public IPv4 address: %w", err)
		}

		ips = append(ips, ip)
	}

	// Add private network IP if available
	for _, privateNet := range server.PrivateNet {
		if privateNet.IP != nil {
			ip, err := netip.ParseAddr(privateNet.IP.String())
			if err != nil {
				return nil, fmt.Errorf("failed to parse private network IP: %w", err)
			}

			ips = append(ips, ip)
		}
	}

	return &provision.NodeInfo{
		ID:   server.Name,
		Name: server.Name,
		Type: nodeType,
		IPs:  ips,
	}, nil
}
