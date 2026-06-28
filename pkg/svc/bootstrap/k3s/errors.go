package k3sbootstrap

import "errors"

var (
	// ErrInvalidRole is returned when a NodeConfig.Role is not one of the three
	// recognised roles (RoleServerInit, RoleServer, RoleAgent). A node's role
	// determines the whole shape of its config, so an unrecognised role cannot be
	// rendered safely.
	ErrInvalidRole = errors.New("k3s: node role must be server-init, server, or agent")

	// ErrMissingToken is returned when a NodeConfig carries no token. Every k3s
	// node — the cluster-initialising server included — needs the shared token to
	// establish or join the cluster, so an empty token is rejected rather than
	// rendered into a config that would fail at startup.
	ErrMissingToken = errors.New("k3s: a node token is required")

	// ErrMissingServerURL is returned when a joining node (RoleServer or RoleAgent)
	// has no ServerURL. A joining node must be told which existing server to
	// register against, so an empty URL is a misconfiguration.
	ErrMissingServerURL = errors.New("k3s: a joining node (server or agent) requires a server URL")

	// ErrServerInitWithServerURL is returned when RoleServerInit is given a
	// ServerURL. The cluster-initialising server starts a new cluster and joins no
	// existing one, so a server URL on it is contradictory and is rejected to catch
	// a topology that would otherwise bootstrap two disjoint clusters.
	ErrServerInitWithServerURL = errors.New(
		"k3s: the cluster-initialising server must not be given a server URL",
	)

	// ErrInvalidServerURL is returned when a joining node's ServerURL is not a
	// well-formed https URL with a host. k3s joins its supervisor over https, so a
	// URL without an https scheme and a host would never connect.
	ErrInvalidServerURL = errors.New("k3s: server URL must be an https URL with a host")

	// ErrAgentServerOnlyOption is returned when an agent node carries a server-only
	// option (TLS SANs, disabled components, or a kubeconfig mode). Those options
	// configure the control plane and have no effect on an agent, so accepting them
	// would silently drop user intent; the misconfiguration is surfaced instead.
	ErrAgentServerOnlyOption = errors.New(
		"k3s: an agent must not set server-only options (TLS SANs, disabled components, kubeconfig mode)",
	)
)
