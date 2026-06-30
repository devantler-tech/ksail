package k3shetzner

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// hetznerInfra is the subset of [hetzner.Provider] operations the provisioner
// uses. Capturing it as an interface lets tests inject a fake provider instead of
// reaching the live Hetzner Cloud API, and documents exactly which provider
// methods this provisioner depends on.
type hetznerInfra interface {
	// EnsureNetwork creates (or returns the existing) private network for the cluster.
	EnsureNetwork(ctx context.Context, clusterName, cidr string) (*hcloud.Network, error)
	// EnsureFirewall creates (or returns the existing) firewall for the cluster.
	EnsureFirewall(
		ctx context.Context,
		clusterName string,
		allowedCIDRs []string,
	) (*hcloud.Firewall, error)
	// EnsurePlacementGroup creates (or returns the existing) placement group, or nil
	// when the strategy disables placement groups.
	EnsurePlacementGroup(
		ctx context.Context,
		clusterName, strategy, name string,
	) (*hcloud.PlacementGroup, error)
	// GetSSHKey returns the named SSH key, or nil when name is empty.
	GetSSHKey(ctx context.Context, name string) (*hcloud.SSHKey, error)
	// NodesExist reports whether any server exists for the cluster.
	NodesExist(ctx context.Context, clusterName string) (bool, error)
	// NetworkExists reports whether the cluster's network exists.
	NetworkExists(ctx context.Context, clusterName string) (bool, error)
	// DeleteNodes removes all servers for the cluster.
	DeleteNodes(ctx context.Context, clusterName string) error
	// StartNodes starts all servers for the cluster.
	StartNodes(ctx context.Context, clusterName string) error
	// StopNodes stops all servers for the cluster.
	StopNodes(ctx context.Context, clusterName string) error
	// ListAllClusters returns the names of all clusters the provider manages.
	ListAllClusters(ctx context.Context) ([]string, error)
}

// staticHetznerInfraCheck asserts at compile time that the concrete Hetzner
// provider satisfies the subset the provisioner depends on.
var _ hetznerInfra = (*hetzner.Provider)(nil)
