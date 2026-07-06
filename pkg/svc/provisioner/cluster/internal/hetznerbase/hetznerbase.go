package hetznerbase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"k8s.io/client-go/kubernetes"
)

var (
	// ErrClusterAlreadyExists is returned by [Base.RunCreate] when servers for the
	// target cluster already exist, so creation would collide with a running cluster.
	ErrClusterAlreadyExists = errors.New("hetzner: cluster already exists")

	// ErrSingleNodePlanExpected is returned by [Base.RunCreate] when a composed
	// [BringUpPlan] does not carry exactly one server spec. Multi-node topologies
	// are rejected before composition ([ErrMultiNodeNotImplemented]), so a plan
	// with any other count is a composition bug, not a user error.
	ErrSingleNodePlanExpected = errors.New(
		"hetzner: bring-up plan must carry exactly one server spec",
	)

	// ErrMissingKubeconfigDestination is returned by [Base.RunCreate] when the
	// Base has no kubeconfig destination path to merge the retrieved admin
	// kubeconfig into. It is checked before any server is created so a
	// misconfiguration cannot leak paid resources.
	ErrMissingKubeconfigDestination = errors.New(
		"hetzner: no kubeconfig destination path is configured",
	)

	// ErrMultiNodeNotImplemented is returned by [Base.Create] for a topology with
	// agents when the distribution's strategy does not yet implement the
	// joining-node bring-up ([MultiNodeComposer]). Both current Hetzner
	// distributions (k3s and kubeadm) implement it, so this guards any future
	// strategy added without join sequencing (see devantler-tech/ksail#5755).
	ErrMultiNodeNotImplemented = errors.New(
		"hetzner: multi-node bring-up is not yet implemented for this distribution (tracked by #5755)",
	)

	// ErrHAControlPlaneNotImplemented is returned by [Base.Create] for a topology with
	// more than one control-plane node when the distribution's strategy does not
	// implement the [HAControlPlaneComposer] capability: additional control planes
	// join the cluster's etcd, whose distribution-specific mechanics (kubeadm's
	// manual certificate distribution vs k3s' embedded etcd) each distribution
	// lifts in its own increment of devantler-tech/ksail#5796 (epic #3983).
	ErrHAControlPlaneNotImplemented = errors.New(
		"hetzner: high-availability (multi-control-plane) bring-up is not yet implemented" +
			" for this distribution (tracked by #5796)",
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

// ServerCreator is the server-creation half of the provider seam, kept separate
// from [Infra] (interface segregation): only the bring-up engine creates
// servers, while every lifecycle operation uses [Infra]. Test doubles for the
// lifecycle never have to fake server creation.
type ServerCreator interface {
	// CreateServer creates a single server from the composed options and waits
	// for the creation to complete.
	CreateServer(ctx context.Context, opts hetzner.CreateServerOpts) (*hcloud.Server, error)
}

// staticInfraCheck asserts at compile time that the concrete Hetzner provider
// satisfies the subsets the provisioners depend on.
var (
	_ Infra         = (*hetzner.Provider)(nil)
	_ ServerCreator = (*hetzner.Provider)(nil)
)

// Base carries the Hetzner infrastructure lifecycle shared by the k3s and kubeadm ×
// Hetzner provisioners: the common configuration and the ResolveName /
// EnsureInfrastructure / Delete / Start / Stop / List / Exists behaviour they use
// identically. Each provisioner embeds a *Base and adds only the parts that differ —
// how it composes per-node user_data and the node token it generates.
type Base struct {
	// Infra is the Hetzner provider seam the lifecycle operations delegate to.
	Infra Infra
	// Servers is the server-creation seam the bring-up engine delegates to
	// (backed by the same provider as Infra in production).
	Servers ServerCreator
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
	// KubeconfigPath is the local kubeconfig file the retrieved admin kubeconfig
	// is merged into after a successful bring-up (the cluster spec's
	// connection.kubeconfig, e.g. "~/.kube/config").
	KubeconfigPath string
	// LogWriter receives the provisioner's progress output.
	LogWriter io.Writer
	// Strategy supplies the distro-specific halves of the create flow ([Create]):
	// per-node composition, the remote kubeconfig path, the distribution label,
	// and the node-token generator. Each provisioner sets itself here at
	// construction so it inherits [Create] by embedding *Base.
	Strategy CreateStrategy
	// BringUpPort is the SSH port the multi-node bring-up dials on a created node;
	// empty means the standard port 22 (the production default — stock OS images
	// run sshd there). The single-node path threads its port through the composed
	// [BringUpPlan] instead; this field lets the multi-node engine, which composes
	// internally, be exercised against a non-standard port.
	BringUpPort string
	// BringUpPollInterval is the delay between the multi-node bring-up's probes for
	// a node's admin kubeconfig; zero means [DefaultKubeconfigPollInterval].
	BringUpPollInterval time.Duration
	// Hub is the hub-cluster clientset the Connector capability publishes the child
	// kubeconfig Secret through (the cluster the KSail operator runs on). Nil outside
	// a pod (the CLI flow), which skips the publish and leaves the Connector read
	// unavailable — a CLI-created Hetzner cluster has no hub to publish to.
	Hub kubernetes.Interface
	// HubNamespace is the hub namespace the Connector kubeconfig Secret lives in
	// (the operator's own namespace). Read only when Hub is set.
	HubNamespace string
	// ConnectorSecretPrefix is the distribution-specific prefix of the Connector
	// kubeconfig Secret name ("<prefix>-<name>-kubeconfig"). Each provisioner sets
	// its own (e.g. "k3s-hetzner") so distributions never collide in the shared hub
	// namespace.
	ConnectorSecretPrefix string
}

// CreateStrategy is the distribution-specific seam [Base.Create] composes with
// the shared Hetzner create flow: only these four pieces differ between the k3s
// and kubeadm provisioners, so each implements this interface and hands itself
// to its embedded [Base].
type CreateStrategy interface {
	// ComposeNodes threads the minted bootstrap material into the distribution's
	// per-node cloud-init user_data and projects it onto the shared [NodeSpec]s.
	ComposeNodes(clusterName, token string, material BootstrapMaterial) ([]NodeSpec, error)
	// RemoteKubeconfigPath is where the distribution writes its admin kubeconfig
	// on the cluster-initialising node.
	RemoteKubeconfigPath() string
	// DistroLabel labels the distribution in the create flow's error context
	// (e.g. "K3s × Hetzner").
	DistroLabel() string
	// GenerateToken produces the cluster's shared node/join token.
	GenerateToken() (string, error)
}

// NewBase constructs a Base, building the Hetzner provider from opts (resolving the
// API token from the configured environment variable). It is the shared provider
// construction both provisioners' NewProvisioner constructors delegate to;
// kubeconfigPath is the local kubeconfig file a successful bring-up merges the
// admin kubeconfig into; LogWriter defaults to os.Stdout.
func NewBase(
	clusterName, kubeconfigPath string,
	controlPlanes, agents int,
	opts v1alpha1.OptionsHetzner,
) (*Base, error) {
	provider, _, err := hetzner.NewProviderFromOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("create Hetzner provider: %w", err)
	}

	return &Base{
		Infra:          provider,
		Servers:        provider,
		Opts:           opts,
		ClusterName:    clusterName,
		ControlPlanes:  controlPlanes,
		Agents:         agents,
		KubeconfigPath: kubeconfigPath,
		LogWriter:      os.Stdout,
	}, nil
}

// Wire finalises a freshly built Base for a distribution provisioner: it records the
// distro-specific create [CreateStrategy] and the Connector Secret prefix. Call it
// once, immediately after [NewBase], passing the constructed provisioner as strategy —
// the wiring step both distributions' NewProvisioner constructors delegate to instead
// of open-coding it (which jscpd flagged as a near-identical clone).
func (b *Base) Wire(strategy CreateStrategy, connectorSecretPrefix string) {
	b.Strategy = strategy
	b.ConnectorSecretPrefix = connectorSecretPrefix
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

// ResolvedInfra carries the IDs of the cluster's shared Hetzner resources after
// [Base.EnsureInfrastructure] created (or reused) them — the placement a composed
// server spec needs. A zero ID means the resource is not in play (no placement
// group for the configured strategy, no SSH key configured, or a provider that
// returned no ID).
type ResolvedInfra struct {
	// NetworkID is the private network every node joins.
	NetworkID int64
	// FirewallID is the firewall applied to every node; zero attaches none.
	FirewallID int64
	// PlacementGroupID is the placement group every node is created in; zero when
	// the strategy disables placement groups.
	PlacementGroupID int64
	// SSHKeyID is the pre-registered Hetzner SSH key installed on every node; zero
	// when no key name is configured. This is Hetzner's account-level key, distinct
	// from the per-cluster bootstrap keypair delivered via cloud-init.
	SSHKeyID int64
}

// EnsureInfrastructure creates (or reuses) the cluster's shared Hetzner resources:
// the private network, the firewall, the placement group, and the SSH key when one
// is configured. It returns their resolved IDs so the server-spec composition can
// place nodes into them.
func (b *Base) EnsureInfrastructure(
	ctx context.Context,
	clusterName string,
) (ResolvedInfra, error) {
	cidr := b.Opts.NetworkCIDR
	if cidr == "" {
		cidr = v1alpha1.DefaultHetznerNetworkCIDR
	}

	resolved := ResolvedInfra{}

	network, err := b.Infra.EnsureNetwork(ctx, clusterName, cidr)
	if err != nil {
		return ResolvedInfra{}, fmt.Errorf("ensure network: %w", err)
	}

	resolved.NetworkID = idOrZero(network, func(n *hcloud.Network) int64 { return n.ID })

	firewall, err := b.Infra.EnsureFirewall(ctx, clusterName, b.Opts.AllowedCIDRs)
	if err != nil {
		return ResolvedInfra{}, fmt.Errorf("ensure firewall: %w", err)
	}

	resolved.FirewallID = idOrZero(firewall, func(f *hcloud.Firewall) int64 { return f.ID })

	placementGroup, err := b.Infra.EnsurePlacementGroup(
		ctx,
		clusterName,
		b.Opts.PlacementGroupStrategy.String(),
		b.Opts.PlacementGroup,
	)
	if err != nil {
		return ResolvedInfra{}, fmt.Errorf("ensure placement group: %w", err)
	}

	resolved.PlacementGroupID = idOrZero(
		placementGroup,
		func(g *hcloud.PlacementGroup) int64 { return g.ID },
	)

	resolved.SSHKeyID, err = b.resolveSSHKeyID(ctx)
	if err != nil {
		return ResolvedInfra{}, err
	}

	return resolved, nil
}

// Delete removes the cluster's servers. It is a no-op (nil) when the cluster's
// network does not exist.
func (b *Base) Delete(ctx context.Context, name string) error {
	clusterName := b.ResolveName(name)

	networkExists, err := b.Infra.NetworkExists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("check network: %w", err)
	}

	if networkExists {
		err = b.Infra.DeleteNodes(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("delete nodes: %w", err)
		}
	}

	// Clean the published Connector Secret on BOTH paths — a retry after the infra
	// is already gone must not orphan the credential in the hub namespace.
	err = b.deleteConnectorKubeconfig(ctx, clusterName)
	if err != nil {
		// The cluster itself is gone; a leftover Secret must not fail the delete.
		// Surface it on the progress output instead.
		_, _ = fmt.Fprintf(b.LogWriter, "warning: %v\n", err)
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
// generate the node token, compose the bring-up plan, and — when the caller returns
// a complete [BringUpPlan] — run the live bring-up: create the server, wait for the
// distribution's admin kubeconfig ([Base.BringUpNode]), rewrite its endpoint to the
// node's public IPv4, and merge it into the Base's kubeconfig destination. The two
// steps that differ between the provisioners are supplied as callbacks:
// generateToken produces the node token, and composePlan composes the per-node
// cloud-init user_data into server specs plus the bootstrap material
// ([DeriveServerSpecs] and [GenerateBootstrapMaterial] are the shared halves
// of that composition).
func (b *Base) RunCreate(
	ctx context.Context,
	name string,
	composePlan func(clusterName, token string, infra ResolvedInfra) (BringUpPlan, error),
	generateToken func() (string, error),
) error {
	clusterName := b.ResolveName(name)

	// RunCreate is the single-node engine; [Base.Create] routes multi-node
	// topologies to [Base.RunCreateMultiNode] before reaching here, so these
	// guards are a defensive backstop for a direct caller.
	if b.ControlPlanes > 1 {
		return ErrHAControlPlaneNotImplemented
	}

	if b.Agents > 0 {
		return ErrMultiNodeNotImplemented
	}

	infra, err := b.guardCreate(ctx, clusterName)
	if err != nil {
		return err
	}

	token, err := generateToken()
	if err != nil {
		return err
	}

	plan, err := composePlan(clusterName, token, infra)
	if err != nil {
		return err
	}

	return b.bringUpFromPlan(ctx, clusterName, plan)
}

// Create runs the whole create flow shared by both provisioners, so neither
// re-writes it. It routes by topology: the single-control-plane, no-agent path
// wires the embedded [CreateStrategy]'s per-node composition into the shared plan
// composition ([PlanComposer]) and runs [Base.RunCreate]; a topology with joining
// nodes runs the two-phase multi-node bring-up ([Base.RunCreateMultiNode]) when
// the strategy implements [MultiNodeComposer] (k3s and kubeadm both do), and is
// rejected otherwise. A multi-control-plane (high-availability) topology
// additionally requires the [HAControlPlaneComposer] capability (kubeadm only)
// and is rejected per-distribution otherwise.
// Each provisioner gets this method by embedding *Base; the distro-specific
// pieces come from the Strategy it sets at construction.
func (b *Base) Create(ctx context.Context, name string) error {
	err := b.create(ctx, name)
	if err != nil {
		return fmt.Errorf("provision %s cluster: %w", b.Strategy.DistroLabel(), err)
	}

	return nil
}

// create dispatches to the single-node or multi-node engine by topology; its
// error is wrapped with the distribution label by [Base.Create].
func (b *Base) create(ctx context.Context, name string) error {
	if b.ControlPlanes > 1 {
		if _, ok := b.Strategy.(HAControlPlaneComposer); !ok {
			return ErrHAControlPlaneNotImplemented
		}
	}

	if b.ControlPlanes > 1 || b.Agents > 0 {
		composer, ok := b.Strategy.(MultiNodeComposer)
		if !ok {
			return ErrMultiNodeNotImplemented
		}

		material, err := GenerateBootstrapMaterial()
		if err != nil {
			return fmt.Errorf("generate bootstrap material: %w", err)
		}

		return b.RunCreateMultiNode(ctx, name, composer, material)
	}

	composePlan := PlanComposer(b.Opts, b.Strategy.RemoteKubeconfigPath(), b.Strategy.ComposeNodes)

	return b.RunCreate(ctx, name, composePlan, b.Strategy.GenerateToken)
}

// guardCreate checks a create flow's preconditions and ensures the shared
// infrastructure, so the single-node ([Base.RunCreate]) and multi-node
// ([Base.RunCreateMultiNode]) engines share one preamble: the cluster must not
// already exist ([ErrClusterAlreadyExists]) and must have a kubeconfig
// destination ([ErrMissingKubeconfigDestination], checked before any paid
// resource exists), after which the network, firewall, placement group, and SSH
// key are ensured ([Base.EnsureInfrastructure]).
func (b *Base) guardCreate(ctx context.Context, clusterName string) (ResolvedInfra, error) {
	exists, err := b.Infra.NodesExist(ctx, clusterName)
	if err != nil {
		return ResolvedInfra{}, fmt.Errorf("check existing nodes: %w", err)
	}

	if exists {
		return ResolvedInfra{}, fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	if b.KubeconfigPath == "" {
		return ResolvedInfra{}, ErrMissingKubeconfigDestination
	}

	return b.EnsureInfrastructure(ctx, clusterName)
}

// rewriteAndPersistKubeconfig rewrites the retrieved kubeconfig's endpoint to the
// created server's public IPv4 and merges it into the Base's kubeconfig
// destination, returning the external endpoint and the persisted path. On any
// failure it tears the cluster down (cleanup-on-failure) so a cluster whose
// credentials cannot be written is not left running. It is shared by the
// single-node and multi-node create flows.
func (b *Base) rewriteAndPersistKubeconfig(
	ctx context.Context,
	clusterName string,
	result BringUpResult,
) (string, string, error) {
	endpoint, err := apiServerEndpoint(result.Server)
	if err != nil {
		return "", "", b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	kubeconfig, err := rewriteKubeconfigEndpoint(result.Kubeconfig, endpoint)
	if err != nil {
		return "", "", b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	persistedPath, err := b.persistKubeconfig(kubeconfig)
	if err != nil {
		return "", "", b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	// Publish the rewritten kubeconfig for the operator's Connector read (a no-op
	// without a hub clientset). Failure tears the cluster down like the other
	// post-bring-up steps: a cluster the operator can never reach would otherwise
	// wedge behind ErrClusterAlreadyExists on the retry.
	err = b.publishConnectorKubeconfig(ctx, clusterName, kubeconfig)
	if err != nil {
		return "", "", b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	return endpoint, persistedPath, nil
}

// bringUpFromPlan runs the live half of the create flow from a composed plan:
// bring the single node up, rewrite the retrieved kubeconfig's endpoint to the
// node's public IPv4, and merge it into the Base's kubeconfig destination. The
// post-bring-up steps share [Base.BringUpNode]'s cleanup-on-failure semantics —
// a cluster whose kubeconfig cannot be rewritten or persisted is torn down again
// rather than left running without credentials (re-running create would only hit
// [ErrClusterAlreadyExists]).
func (b *Base) bringUpFromPlan(
	ctx context.Context,
	clusterName string,
	plan BringUpPlan,
) error {
	if len(plan.Specs) != 1 {
		return fmt.Errorf("%w: got %d", ErrSingleNodePlanExpected, len(plan.Specs))
	}

	result, err := b.BringUpNode(ctx, clusterName, BringUpSpec{
		Server:          plan.Specs[0],
		Signer:          plan.Signer,
		HostKeyCallback: plan.HostKeyCallback,
		KubeconfigPath:  plan.RemoteKubeconfigPath,
		PollInterval:    plan.PollInterval,
		Port:            plan.Port,
	})
	if err != nil {
		return err
	}

	endpoint, persistedPath, err := b.rewriteAndPersistKubeconfig(ctx, clusterName, result)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(
		b.LogWriter,
		"Cluster %q is up at %s; kubeconfig merged into %q\n",
		clusterName, endpoint, persistedPath,
	)

	return nil
}

// resolveSSHKeyID resolves the configured Hetzner SSH key name to its ID, or
// zero when no key name is configured.
func (b *Base) resolveSSHKeyID(ctx context.Context) (int64, error) {
	if b.Opts.SSHKeyName == "" {
		return 0, nil
	}

	sshKey, err := b.Infra.GetSSHKey(ctx, b.Opts.SSHKeyName)
	if err != nil {
		return 0, fmt.Errorf("get SSH key: %w", err)
	}

	return idOrZero(sshKey, func(k *hcloud.SSHKey) int64 { return k.ID }), nil
}

// idOrZero returns id(resource), or zero when the provider returned no
// resource (e.g. a placement-group strategy that disables placement groups).
func idOrZero[T any](resource *T, id func(*T) int64) int64 {
	if resource == nil {
		return 0
	}

	return id(resource)
}
