package k3sbootstrap

// PlanInput describes the topology of a K3s cluster to bootstrap across a set of
// freshly-provisioned servers (e.g. Hetzner Cloud servers). It is the typed input
// for [Plan], which expands it into the ordered per-node [InstallConfig]s a
// provisioner renders and delivers.
type PlanInput struct {
	// Version pins the k3s release every node installs (INSTALL_K3S_VERSION).
	// Required — see ErrMissingVersion.
	Version string

	// Token is the shared node token every node authenticates with (K3S_TOKEN).
	// Required — see ErrMissingToken.
	Token string

	// ControlPlaneCount is the number of control-plane (server) nodes. Must be at
	// least one: the first becomes the cluster-initialising server (RoleServerInit)
	// and any further ones join it (RoleServer). See ErrInvalidControlPlaneCount.
	ControlPlaneCount int

	// AgentCount is the number of worker (agent) nodes. May be zero; must not be
	// negative. See ErrInvalidAgentCount.
	AgentCount int

	// ServerURL is the registration endpoint joining nodes dial, e.g. the first
	// server's address or a load balancer ("https://10.0.0.2:6443"). It is required
	// whenever the plan contains a joining node (more than one control-plane node,
	// or any agent) and is never applied to the cluster-initialising server. See
	// ErrMissingServerURL.
	ServerURL string

	// TLSSANs are additional Subject Alternative Names added to every control-plane
	// node's API server certificate (--tls-san). Applied to server roles only.
	TLSSANs []string

	// Disable lists bundled components every control-plane node disables
	// (--disable). Applied to server roles only.
	Disable []string

	// WriteKubeconfigMode sets the generated kubeconfig's mode on every
	// control-plane node (--write-kubeconfig-mode). Optional; server roles only.
	WriteKubeconfigMode string
}

// Node is a single server's place in the bootstrap order together with the
// install configuration it must run.
type Node struct {
	// Index is the node's zero-based position in bootstrap order: the
	// cluster-initialising server is 0, additional servers follow, then agents.
	Index int

	// Config is the install configuration for this node, already assigned the
	// correct Role and join settings. It is guaranteed to pass validation, so
	// [Render] never fails for a Config returned by [Plan].
	Config InstallConfig
}

// Plan expands a [PlanInput] into the ordered [Node]s that bootstrap the cluster:
// the cluster-initialising server first (RoleServerInit), then any additional
// control-plane servers (RoleServer), then the agents (RoleAgent). The order is
// significant — joining nodes can only register once the first server has
// initialised the cluster — so callers should provision and bootstrap the nodes
// in the returned sequence.
//
// Plan is pure: it performs no I/O and reaches no network. Every returned
// Node.Config is valid, so [Render] applied to it never returns an error. A
// configuration error (a missing version or token, an invalid node count, or a
// missing ServerURL when joining nodes are present) is reported instead of a
// partial plan.
func Plan(input PlanInput) ([]Node, error) {
	err := input.validate()
	if err != nil {
		return nil, err
	}

	nodes := make([]Node, 0, input.ControlPlaneCount+input.AgentCount)

	// The first control-plane node initialises the cluster and must not carry a
	// ServerURL; the remaining control-plane nodes join it.
	nodes = append(nodes, Node{Index: 0, Config: input.serverConfig(RoleServerInit, "")})

	for range input.ControlPlaneCount - 1 {
		nodes = append(nodes, Node{
			Index:  len(nodes),
			Config: input.serverConfig(RoleServer, input.ServerURL),
		})
	}

	for range input.AgentCount {
		nodes = append(nodes, Node{
			Index: len(nodes),
			Config: InstallConfig{
				Version:   input.Version,
				Role:      RoleAgent,
				Token:     input.Token,
				ServerURL: input.ServerURL,
			},
		})
	}

	return nodes, nil
}

// validate reports the first error in input, or nil when it describes a cluster
// Plan can fully expand. ServerURL is required only when the plan contains a
// joining node, mirroring InstallConfig.validate for the individual roles.
func (input PlanInput) validate() error {
	if input.Version == "" {
		return ErrMissingVersion
	}

	if input.Token == "" {
		return ErrMissingToken
	}

	if input.ControlPlaneCount < 1 {
		return ErrInvalidControlPlaneCount
	}

	if input.AgentCount < 0 {
		return ErrInvalidAgentCount
	}

	if input.hasJoiningNodes() && input.ServerURL == "" {
		return ErrMissingServerURL
	}

	return nil
}

// hasJoiningNodes reports whether the plan contains any node that must register
// against an existing server: an additional control-plane node or any agent.
func (input PlanInput) hasJoiningNodes() bool {
	return input.ControlPlaneCount > 1 || input.AgentCount > 0
}

// serverConfig builds the InstallConfig for a control-plane node in the given
// role, carrying the shared server-only options (TLS SANs, disables, kubeconfig
// mode). serverURL is empty for RoleServerInit and the join endpoint for
// RoleServer.
func (input PlanInput) serverConfig(role Role, serverURL string) InstallConfig {
	return InstallConfig{
		Version:             input.Version,
		Role:                role,
		Token:               input.Token,
		ServerURL:           serverURL,
		TLSSANs:             input.TLSSANs,
		Disable:             input.Disable,
		WriteKubeconfigMode: input.WriteKubeconfigMode,
	}
}
