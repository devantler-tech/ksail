package k3sbootstrap

import "errors"

var (
	// ErrMissingVersion is returned when InstallConfig.Version is empty. A pinned
	// k3s version is required so a node's Kubernetes version is reproducible and
	// never silently tracks the upstream "latest" channel.
	ErrMissingVersion = errors.New("k3s install: a pinned version is required")

	// ErrMissingToken is returned when InstallConfig.Token is empty. Every k3s node
	// — the first server, additional servers, and agents — authenticates to the
	// cluster with the shared node token.
	ErrMissingToken = errors.New("k3s install: token is required")

	// ErrMissingServerURL is returned when a joining node (an additional server or
	// an agent) has no ServerURL to register against. Only the first server
	// (RoleServerInit) bootstraps without one.
	ErrMissingServerURL = errors.New(
		"k3s install: server URL is required to join an existing cluster",
	)

	// ErrUnexpectedServerURL is returned when RoleServerInit is given a ServerURL.
	// The cluster-initialising server defines the join endpoint; it does not join
	// another, so a ServerURL here signals a misconfiguration.
	ErrUnexpectedServerURL = errors.New(
		"k3s install: the cluster-init server must not be given a server URL",
	)

	// ErrAgentServerOnlyOption is returned when an agent is given a server-only
	// option (TLS SANs or component disables). Those configure the control plane
	// and have no effect on an agent, so accepting them silently would mislead.
	ErrAgentServerOnlyOption = errors.New(
		"k3s install: TLS SANs and disabled components are server-only options",
	)

	// ErrUnknownRole is returned when InstallConfig.Role is not one of the defined
	// roles.
	ErrUnknownRole = errors.New("k3s install: unknown node role")
)
