package kubeadmbootstrap

import (
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/internal/sliceutil"
)

// PlanInput describes the topology of a kubeadm cluster to bootstrap across a set
// of freshly-provisioned servers (e.g. Hetzner Cloud servers). It is the typed
// input for [Plan], which expands it into the ordered per-node [NodeConfig]s a
// provisioner renders and delivers. It mirrors the k3sbootstrap planner so a
// provisioner can plan either distribution's topology from one shared shape.
type PlanInput struct {
	// Token is the shared bootstrap token every node authenticates with — issued on
	// the cluster-initialising control plane and redeemed by every joining node.
	// Required — see [ErrMissingToken].
	Token string

	// KubernetesVersion pins the control-plane image set on the
	// cluster-initialising control plane (e.g. "v1.31.0"). Optional and
	// cluster-wide: it is applied to the server-init node only.
	KubernetesVersion string
	// ControlPlaneEndpoint is the stable "host:port" the cluster advertises for its
	// API server, enabling later control-plane nodes to join. Optional and
	// cluster-wide: applied to the server-init node only.
	ControlPlaneEndpoint string
	// CertSANs are extra Subject Alternative Names added to the API server
	// certificate. Cluster-wide: applied to the server-init node only. Cloned per
	// node so a downstream mutation cannot corrupt another node's config.
	CertSANs []string
	// PodSubnet is the pod network CIDR. Cluster-wide: applied to the server-init
	// node only.
	PodSubnet string
	// ServiceSubnet is the service network CIDR. Cluster-wide: applied to the
	// server-init node only.
	ServiceSubnet string

	// ControlPlaneCount is the number of control-plane (server) nodes. Must be at
	// least one: the first becomes the cluster-initialising control plane
	// (RoleServerInit) and any further ones join it (RoleServer). See
	// [ErrInvalidControlPlaneCount].
	ControlPlaneCount int
	// AgentCount is the number of worker (agent) nodes. May be zero; must not be
	// negative. See [ErrInvalidAgentCount].
	AgentCount int

	// APIServerEndpoint is the "host:port" of the cluster-initialising control
	// plane that joining nodes register against. Required whenever the plan
	// contains a joining node (more than one control-plane node, or any agent) and
	// is never applied to the cluster-initialising control plane. Because kubeadm
	// discovers it only once the first control plane is up, a provisioner supplies
	// it at run time before planning the joining nodes. See
	// [ErrMissingAPIServerEndpoint] / [ErrInvalidAPIServerEndpoint].
	APIServerEndpoint string
	// CACertHashes are the pinned "sha256:<hex>" cluster-CA hashes a joining node
	// verifies during token discovery. Required (at least one) whenever the plan
	// contains a joining node, and — like [APIServerEndpoint] — is a run-time
	// artifact of the cluster-initialising control plane that a provisioner injects
	// before planning the joining nodes. Cloned per node. See
	// [ErrMissingCACertHashes] / [ErrInvalidCACertHash].
	CACertHashes []string
}

// Node is a single server's place in the bootstrap order together with the
// kubeadm configuration it must run.
type Node struct {
	// Index is the node's zero-based position in bootstrap order: the
	// cluster-initialising control plane is 0, additional control planes follow,
	// then agents.
	Index int

	// Config is the kubeadm configuration for this node, already assigned the
	// correct Role and join settings. It is guaranteed to pass validation, so
	// [Render] never fails for a Config returned by [Plan].
	Config NodeConfig
}

// Plan expands a [PlanInput] into the ordered [Node]s that bootstrap the cluster:
// the cluster-initialising control plane first (RoleServerInit), then any
// additional control-plane nodes (RoleServer), then the agents (RoleAgent). The
// order is significant — joining nodes can only register once the first control
// plane has initialised the cluster — so callers should provision and bootstrap
// the nodes in the returned sequence.
//
// Plan is pure: it performs no I/O and reaches no network. Every returned
// Node.Config is valid, so [Render] applied to it never returns an error. A
// configuration error (a missing token, an invalid node count, or a missing or
// malformed API server endpoint / CA cert hash when joining nodes are present) is
// reported instead of a partial plan.
func Plan(input PlanInput) ([]Node, error) {
	nodes, err := sliceutil.ValidateAndPrealloc[Node](input.validate, input.nodeCount())
	if err != nil {
		return nil, err
	}

	// The first control-plane node initialises the cluster and joins nothing; the
	// remaining control-plane nodes and the agents join it.
	nodes = sliceutil.AssignNodes(
		nodes, input.ControlPlaneCount, input.AgentCount,
		func(index int, config NodeConfig) Node { return Node{Index: index, Config: config} },
		input.serverInitConfig,
		func() NodeConfig { return input.joinConfig(RoleServer) },
		func() NodeConfig { return input.joinConfig(RoleAgent) },
	)

	return nodes, nil
}

// nodeCount is the total number of nodes (control-plane + agent) the plan expands to —
// the capacity Plan preallocates its result slice with.
func (input PlanInput) nodeCount() int {
	return input.ControlPlaneCount + input.AgentCount
}

// validate reports the first error in input, or nil when it describes a cluster
// Plan can fully expand. The API server endpoint and CA cert hashes are required
// (and validated) only when the plan contains a joining node, mirroring
// [NodeConfig.validate] for the individual roles so every produced Config is
// guaranteed to render.
func (input PlanInput) validate() error {
	if input.Token == "" {
		return ErrMissingToken
	}

	if input.ControlPlaneCount < 1 {
		return ErrInvalidControlPlaneCount
	}

	if input.AgentCount < 0 {
		return ErrInvalidAgentCount
	}

	if input.hasJoiningNodes() {
		err := validateAPIServerEndpoint(input.APIServerEndpoint)
		if err != nil {
			return err
		}

		return validateCACertHashes(input.CACertHashes)
	}

	return nil
}

// hasJoiningNodes reports whether the plan contains any node that must register
// against the cluster-initialising control plane: an additional control-plane
// node or any agent.
func (input PlanInput) hasJoiningNodes() bool {
	return input.ControlPlaneCount > 1 || input.AgentCount > 0
}

// serverInitConfig builds the NodeConfig for the cluster-initialising control
// plane, carrying the cluster-wide options (kubernetes version, control-plane
// endpoint, cert SANs, pod/service subnets) and no join fields.
//
// CertSANs is cloned: a single PlanInput expands into one server-init config, but
// sharing the input's slice header would let a downstream mutation of the node's
// slice corrupt the caller's input. slices.Clone preserves nil, so an unset field
// stays nil rather than becoming an empty slice.
func (input PlanInput) serverInitConfig() NodeConfig {
	return NodeConfig{
		Role:                 RoleServerInit,
		Token:                input.Token,
		KubernetesVersion:    input.KubernetesVersion,
		ControlPlaneEndpoint: input.ControlPlaneEndpoint,
		CertSANs:             slices.Clone(input.CertSANs),
		PodSubnet:            input.PodSubnet,
		ServiceSubnet:        input.ServiceSubnet,
	}
}

// joinConfig builds the NodeConfig for a joining node in the given role
// (RoleServer for an additional control plane, RoleAgent for a worker), carrying
// the shared discovery settings (API server endpoint and CA cert hashes) and no
// cluster-wide options. CACertHashes is cloned per config so sharing the input's
// slice header cannot let one node's config corrupt another's.
func (input PlanInput) joinConfig(role Role) NodeConfig {
	return NodeConfig{
		Role:              role,
		Token:             input.Token,
		APIServerEndpoint: input.APIServerEndpoint,
		CACertHashes:      slices.Clone(input.CACertHashes),
	}
}
