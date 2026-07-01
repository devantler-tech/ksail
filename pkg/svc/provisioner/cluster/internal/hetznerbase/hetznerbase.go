package hetznerbase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

var (
	// ErrClusterAlreadyExists is returned by [Base.RunCreate] when servers for the
	// target cluster already exist, so creation would collide with a running cluster.
	ErrClusterAlreadyExists = errors.New("hetzner: cluster already exists")

	// ErrLiveBringUpNotImplemented is returned by [Base.RunCreate] after the shared
	// infrastructure is ensured and the per-node cloud-init user_data is composed.
	// The remaining steps — creating the servers (which needs boot-image resolution),
	// the runtime join sequencing that depends on the first node's address, and
	// retrieving the generated kubeconfig — are integration paths that land with the
	// Hetzner system-test lane (devantler-tech/ksail#5515).
	ErrLiveBringUpNotImplemented = errors.New(
		"hetzner: live cluster bring-up is not yet implemented (tracked by #5515)",
	)

	// ErrMultiNodeNotImplemented is returned by [Base.RunCreate] for a topology with
	// joining nodes (more than one control-plane node, or any agent). Joining nodes
	// register against the first node's address, which is only known once that node
	// is running, so multi-node bring-up requires the runtime sequencing tracked by
	// devantler-tech/ksail#5515.
	ErrMultiNodeNotImplemented = errors.New(
		"hetzner: multi-node bring-up is not yet implemented (tracked by #5515)",
	)
)

// Infra is the subset of [hetzner.Provider] operations the Hetzner provisioners
// use. Capturing it as an interface lets tests inject a fake provider instead of
// reaching the live Hetzner Cloud API, and documents exactly which provider methods
// the provisioners depend on. The k3s and kubeadm × Hetzner provisioners share this
// seam: they use the same Hetzner infrastructure lifecycle and differ only in the
// per-node user_data they compose.
type Infra interface {
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

// staticInfraCheck asserts at compile time that the concrete Hetzner provider
// satisfies the subset the provisioners depend on.
var _ Infra = (*hetzner.Provider)(nil)

// Base carries the Hetzner infrastructure lifecycle shared by the k3s and kubeadm ×
// Hetzner provisioners: the common configuration and the ResolveName /
// EnsureInfrastructure / Delete / Start / Stop / List / Exists behaviour they use
// identically. Each provisioner embeds a *Base and adds only the parts that differ —
// how it composes per-node user_data and the node token it generates.
type Base struct {
	// Infra is the Hetzner provider seam the lifecycle operations delegate to.
	Infra Infra
	// Opts is the resolved Hetzner options (network CIDR, firewall CIDRs, placement
	// group, SSH key) the infrastructure step reads.
	Opts v1alpha1.OptionsHetzner
	// ClusterName is the default cluster name used when an operation is called with
	// an empty name.
	ClusterName string
	// ControlPlanes is the requested control-plane node count.
	ControlPlanes int
	// Agents is the requested agent (worker) node count.
	Agents int
	// LogWriter receives the provisioner's progress output.
	LogWriter io.Writer
}

// NewBase constructs a Base, building the Hetzner provider from opts (resolving the
// API token from the configured environment variable). It is the shared provider
// construction both provisioners' NewProvisioner constructors delegate to;
// LogWriter defaults to os.Stdout.
func NewBase(
	clusterName string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
) (*Base, error) {
	provider, _, err := hetzner.NewProviderFromOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("create Hetzner provider: %w", err)
	}

	return &Base{
		Infra:         provider,
		Opts:          opts,
		ClusterName:   clusterName,
		ControlPlanes: controlPlanes,
		Agents:        agents,
		LogWriter:     os.Stdout,
	}, nil
}

// ResolveName returns name when non-empty, otherwise the Base's configured default
// cluster name, matching the Provisioner interface's "empty name means use config
// default" contract.
func (b *Base) ResolveName(name string) string {
	if name != "" {
		return name
	}

	return b.ClusterName
}

// EnsureInfrastructure creates (or reuses) the cluster's shared Hetzner resources:
// the private network, the firewall, the placement group, and the SSH key when one
// is configured.
func (b *Base) EnsureInfrastructure(ctx context.Context, clusterName string) error {
	cidr := b.Opts.NetworkCIDR
	if cidr == "" {
		cidr = v1alpha1.DefaultHetznerNetworkCIDR
	}

	_, err := b.Infra.EnsureNetwork(ctx, clusterName, cidr)
	if err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	_, err = b.Infra.EnsureFirewall(ctx, clusterName, b.Opts.AllowedCIDRs)
	if err != nil {
		return fmt.Errorf("ensure firewall: %w", err)
	}

	_, err = b.Infra.EnsurePlacementGroup(
		ctx,
		clusterName,
		b.Opts.PlacementGroupStrategy.String(),
		b.Opts.PlacementGroup,
	)
	if err != nil {
		return fmt.Errorf("ensure placement group: %w", err)
	}

	if b.Opts.SSHKeyName != "" {
		_, err = b.Infra.GetSSHKey(ctx, b.Opts.SSHKeyName)
		if err != nil {
			return fmt.Errorf("get SSH key: %w", err)
		}
	}

	return nil
}

// Delete removes the cluster's servers. It is a no-op (nil) when the cluster's
// network does not exist.
func (b *Base) Delete(ctx context.Context, name string) error {
	clusterName := b.ResolveName(name)

	networkExists, err := b.Infra.NetworkExists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("check network: %w", err)
	}

	if !networkExists {
		return nil
	}

	err = b.Infra.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("delete nodes: %w", err)
	}

	return nil
}

// Start starts the cluster's servers.
func (b *Base) Start(ctx context.Context, name string) error {
	err := b.Infra.StartNodes(ctx, b.ResolveName(name))
	if err != nil {
		return fmt.Errorf("start nodes: %w", err)
	}

	return nil
}

// Stop stops the cluster's servers.
func (b *Base) Stop(ctx context.Context, name string) error {
	err := b.Infra.StopNodes(ctx, b.ResolveName(name))
	if err != nil {
		return fmt.Errorf("stop nodes: %w", err)
	}

	return nil
}

// List returns the names of all clusters the Hetzner provider manages.
func (b *Base) List(ctx context.Context) ([]string, error) {
	clusters, err := b.Infra.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}

	return clusters, nil
}

// Exists reports whether servers exist for the named cluster.
func (b *Base) Exists(ctx context.Context, name string) (bool, error) {
	exists, err := b.Infra.NodesExist(ctx, b.ResolveName(name))
	if err != nil {
		return false, fmt.Errorf("check nodes exist: %w", err)
	}

	return exists, nil
}

// RunCreate runs the Hetzner create flow shared by the k3s and kubeadm provisioners:
// guard against an existing cluster ([ErrClusterAlreadyExists]), reject multi-node
// topologies ([ErrMultiNodeNotImplemented]), ensure the shared infrastructure,
// generate the node token, compose the per-node cloud-init user_data, and stop at
// the live-bring-up boundary ([ErrLiveBringUpNotImplemented], see
// devantler-tech/ksail#5515). The two steps that differ between the provisioners are
// supplied as callbacks: generateToken produces the node token, and composeNodes
// composes the per-node user_data and returns the node count.
func (b *Base) RunCreate(
	ctx context.Context,
	name string,
	composeNodes func(clusterName, token string) (int, error),
	generateToken func() (string, error),
) error {
	clusterName := b.ResolveName(name)

	exists, err := b.Infra.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("check existing nodes: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	if b.ControlPlanes > 1 || b.Agents > 0 {
		return ErrMultiNodeNotImplemented
	}

	err = b.EnsureInfrastructure(ctx, clusterName)
	if err != nil {
		return err
	}

	token, err := generateToken()
	if err != nil {
		return err
	}

	count, err := composeNodes(clusterName, token)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(
		b.LogWriter,
		"Prepared cloud-init bootstrap for %d node(s); server creation and "+
			"kubeconfig retrieval are tracked by #5515\n",
		count,
	)

	return ErrLiveBringUpNotImplemented
}
