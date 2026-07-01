package kubeadmbootstrap

import "errors"

var (
	// ErrInvalidRole is returned when a NodeConfig.Role is not one of the three
	// recognised roles (RoleServerInit, RoleServer, RoleAgent). A node's role
	// determines whether an Init/Cluster or a Join configuration is rendered, so an
	// unrecognised role cannot be rendered safely.
	ErrInvalidRole = errors.New("kubeadm: node role must be server-init, server, or agent")

	// ErrMissingToken is returned when a NodeConfig carries no bootstrap token.
	// Every kubeadm node — the cluster-initialising control plane included — needs
	// the shared token to issue or redeem the join credential, so an empty token is
	// rejected rather than rendered into a config that would fail at bring-up.
	ErrMissingToken = errors.New("kubeadm: a bootstrap token is required")

	// ErrServerInitWithJoinFields is returned when RoleServerInit carries a
	// join-only field (an API server endpoint or CA cert hashes). The
	// cluster-initialising control plane starts a new cluster and joins no existing
	// one, so discovery fields on it are contradictory and are rejected to catch a
	// topology that would otherwise try to both start and join a cluster.
	ErrServerInitWithJoinFields = errors.New(
		"kubeadm: the cluster-initialising control plane must not be given join fields " +
			"(API server endpoint or CA cert hashes)",
	)

	// ErrMissingAPIServerEndpoint is returned when a joining node (RoleServer or
	// RoleAgent) has no APIServerEndpoint. A joining node must be told which
	// existing control plane to register against, so an empty endpoint is a
	// misconfiguration.
	ErrMissingAPIServerEndpoint = errors.New(
		"kubeadm: a joining node (server or agent) requires an API server endpoint",
	)

	// ErrInvalidAPIServerEndpoint is returned when a joining node's
	// APIServerEndpoint is not a well-formed host:port. kubeadm dials the endpoint
	// directly during discovery, so a value without a host and a numeric port would
	// never connect.
	ErrInvalidAPIServerEndpoint = errors.New(
		"kubeadm: API server endpoint must be a host:port with a numeric port",
	)

	// ErrMissingCACertHashes is returned when a joining node carries no CA cert
	// hashes. Token discovery authenticates the cluster CA with a pinned
	// "sha256:..." hash; omitting it would require the insecure
	// unsafeSkipCAVerification path, so the safe pinned form is required instead.
	ErrMissingCACertHashes = errors.New(
		"kubeadm: a joining node requires at least one CA cert hash (sha256:...)",
	)

	// ErrInvalidCACertHash is returned when a CA cert hash is not in the
	// "sha256:<hex>" form kubeadm expects. An unparseable pin would be rejected at
	// join time, so it is surfaced at render time instead.
	ErrInvalidCACertHash = errors.New(
		`kubeadm: each CA cert hash must be of the form "sha256:<hex>"`,
	)

	// ErrServerInitOnlyOption is returned when a joining node (RoleServer or
	// RoleAgent) carries a cluster-wide option (kubernetes version, control-plane
	// endpoint, cert SANs, or pod/service subnet). Those configure the cluster the
	// initialising control plane creates and have no effect on a joining node, so
	// accepting them would silently drop user intent; the misconfiguration is
	// surfaced instead.
	ErrServerInitOnlyOption = errors.New(
		"kubeadm: a joining node must not set cluster-wide options " +
			"(kubernetes version, control-plane endpoint, cert SANs, pod/service subnet)",
	)

	// ErrMissingKubernetesVersion is returned by [RenderInstall] when
	// InstallConfig.KubernetesVersion is empty. The community package repository is
	// published per minor version, so the version cannot be defaulted at install
	// time — without it there is no repository track to point the node at.
	ErrMissingKubernetesVersion = errors.New(
		"kubeadm: a Kubernetes version is required to select the package repository",
	)

	// ErrInvalidKubernetesVersion is returned by [RenderInstall] when
	// InstallConfig.KubernetesVersion is not a "vMAJOR.MINOR[.PATCH]" version with
	// numeric major and minor components. Its minor track selects a repository URL,
	// so a malformed version would point the node at a URL that does not resolve;
	// it is rejected at render time instead.
	ErrInvalidKubernetesVersion = errors.New(
		"kubeadm: Kubernetes version must be of the form vMAJOR.MINOR[.PATCH]",
	)

	// ErrMissingConfig is returned by [RenderInstall] when InstallConfig.Config is
	// empty. The install drops the node's kubeadm configuration and bootstraps from
	// it, so an empty config would render an install that runs `kubeadm` against a
	// non-existent file.
	ErrMissingConfig = errors.New(
		"kubeadm: a rendered kubeadm configuration is required to install a node",
	)
)
