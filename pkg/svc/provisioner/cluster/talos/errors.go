package talosprovisioner

import "errors"

// Common errors for the Talos provisioner.
var (
	// ErrDockerNotAvailable is returned when Docker is not available.
	ErrDockerNotAvailable = errors.New("docker is not available: ensure Docker is running")
	// ErrClusterAlreadyExists is returned when attempting to create a cluster that already exists.
	ErrClusterAlreadyExists = errors.New("cluster already exists")
	// ErrNodeNoReachableAddress is returned when a Hetzner server has neither a public
	// IPv4 nor a private-network IP, so KSail has no address to reach its Talos API.
	ErrNodeNoReachableAddress = errors.New("hetzner node has no reachable address")
	// ErrHetznerServerMissingFromInventory is returned when the infrastructure
	// inventory names a server that the authoritative Hetzner lookup cannot find.
	ErrHetznerServerMissingFromInventory = errors.New(
		"hetzner server disappeared from authoritative inventory",
	)
	// ErrFloatingIPMissingForControlPlaneConfig is returned when floating-IP
	// enablement requires a first-boot control-plane config refresh but the
	// previously reconciled, cluster-owned address cannot be found.
	ErrFloatingIPMissingForControlPlaneConfig = errors.New(
		"cluster-owned floating IP missing before control-plane config apply",
	)
	// ErrPrivateNetworkUnreachable is returned when KSail cannot reach an IPv4-less
	// Hetzner node's Talos API over the private network — typically because KSail has
	// no route into the private network or the node lacks egress.
	ErrPrivateNetworkUnreachable = errors.New(
		"hetzner private network is unreachable from ksail",
	)
	// ErrInvalidPatch is returned when a patch file is invalid.
	ErrInvalidPatch = errors.New("invalid patch file")
	// ErrStorageHealthTimeout is returned when the opt-in between-node storage-health
	// gate times out waiting for replicated-storage volumes to return to a healthy
	// state during a rolling reboot (see spec.cluster.talos.storageHealthTimeout).
	ErrStorageHealthTimeout = errors.New(
		"timed out waiting for storage volumes to become healthy between nodes",
	)
	// ErrNotImplemented is returned when a method is not yet implemented.
	ErrNotImplemented = errors.New("not implemented")
	// ErrIPv6NotSupported is returned when IPv6 addresses are used but not supported.
	ErrIPv6NotSupported = errors.New("IPv6 not supported")
	// ErrNegativeOffset is returned when a negative offset is provided for IP calculation.
	ErrNegativeOffset = errors.New("negative offset not allowed")
	// ErrNoControlPlane is returned when no control plane container is found.
	ErrNoControlPlane = errors.New("no control plane container found")
	// ErrNoPortMapping is returned when no port mapping is found for a required port.
	ErrNoPortMapping = errors.New("no port mapping found")
	// ErrHetznerProviderRequired is returned when the Hetzner provider is expected but not available.
	ErrHetznerProviderRequired = errors.New("hetzner provider required for this operation")
	// ErrOmniProviderRequired is returned when the Omni provider is expected but not available.
	ErrOmniProviderRequired = errors.New("omni provider required for this operation")
	// ErrMissingKubernetesEndpoint is returned when the cluster info is missing the Kubernetes endpoint.
	ErrMissingKubernetesEndpoint = errors.New("cluster info missing KubernetesEndpoint")
	// ErrKernelModuleLoadFailed is returned when loading a required kernel module fails.
	ErrKernelModuleLoadFailed = errors.New("failed to load kernel module")
	// ErrMinimumControlPlanes is returned when scaling would reduce control-plane nodes below 1.
	ErrMinimumControlPlanes = errors.New("cannot scale control-plane nodes below 1")
	// ErrEtcdLeaveCluster is returned when an etcd member fails to leave the cluster.
	ErrEtcdLeaveCluster = errors.New("failed to remove etcd member")
	// ErrNoConfigForRole is returned when no Talos machine config is available for a role.
	ErrNoConfigForRole = errors.New("no config available for role")
	// ErrUpgradeFailed is returned when a Talos OS upgrade fails on a node.
	ErrUpgradeFailed = errors.New("talos upgrade failed")
	// ErrNodeNotReady is returned when a node does not become ready within the timeout.
	ErrNodeNotReady = errors.New("node did not become ready within timeout")
	// ErrEmptyVersionResponse is returned when a Talos node returns an empty version response.
	ErrEmptyVersionResponse = errors.New("empty version response from node")
	// ErrNoControlPlaneForRefresh is returned when no control-plane node can be found
	// for kubeconfig refresh.
	ErrNoControlPlaneForRefresh = errors.New("no control-plane node found for kubeconfig refresh")
	// ErrSchematicRequiresVersion is returned when a schematicId is set but talos.version is empty.
	ErrSchematicRequiresVersion = errors.New(
		"spec.cluster.talos.version must be set when spec.cluster.talos.schematicId is set",
	)
	// ErrARM64SnapshotNotSupported is returned when snapshot-based boot is requested with ARM64
	// Hetzner server types (cax*). ARM64 snapshot support is not yet implemented.
	ErrARM64SnapshotNotSupported = errors.New(
		"snapshot-based boot (schematicId) does not support ARM64 server types (cax*) yet",
	)
	// ErrNilWorkerConfig is returned when a nil worker config is passed to
	// GenerateAutoscalerWorkerConfig.
	ErrNilWorkerConfig = errors.New("worker config must not be nil")
	// ErrNoControlPlaneForSecretSync is returned when no running control-plane node can be
	// found to extract PKI secrets from during cluster update.
	ErrNoControlPlaneForSecretSync = errors.New(
		"no running control-plane node found for secret sync: " +
			"cannot reuse cluster PKI — new or updated nodes would receive mismatched certificates",
	)
	// ErrAutoscalerRequiresSchematic is returned when the node autoscaler is enabled
	// but no schematic (extensions or explicit schematicId) is configured. The autoscaler
	// needs a Hetzner snapshot image to boot new nodes.
	ErrAutoscalerRequiresSchematic = errors.New(
		"node autoscaler requires spec.cluster.talos.schematicId or " +
			"spec.cluster.talos.extensions to create a boot snapshot for new nodes",
	)
	// ErrHcloudTokenNotSet is returned when the Hetzner Cloud API token environment
	// variable is not set but is required for autoscaler secret creation.
	ErrHcloudTokenNotSet = errors.New("hcloud API token environment variable is not set")
	// ErrAutoscalerUserDataTooLarge is returned when the gzip-compressed,
	// base64-encoded autoscaler worker config still exceeds Hetzner's 32 KiB
	// user_data limit. Hetzner would otherwise reject every scale-up with
	// "invalid input in field 'user_data'"; failing here surfaces the problem at
	// cluster create/update time instead of silently at the next scale-up.
	ErrAutoscalerUserDataTooLarge = errors.New(
		"autoscaler worker config exceeds Hetzner's 32 KiB user_data limit even after gzip " +
			"compression: move large inline patches or extraManifests out of the Talos worker " +
			"config and deliver them via GitOps",
	)
	// ErrDrainPodRetrieval is returned when listing pods for drain encounters errors.
	ErrDrainPodRetrieval = errors.New("failed to retrieve pods for drain")
	// ErrNodeNotFoundByIP is returned when no Kubernetes node matches the given IP.
	ErrNodeNotFoundByIP = errors.New("no Kubernetes node found with IP")
	// ErrConfigNilForInsecureApply is returned when a nil config is passed to applyConfigInsecure.
	ErrConfigNilForInsecureApply = errors.New("config must not be nil for insecure apply")
	// ErrStartNotSupported is returned when Start is called on a Talos-on-Kubernetes provisioner.
	ErrStartNotSupported = errors.New(
		"start not supported for Talos-on-Kubernetes: recreate the cluster instead",
	)
	// ErrStopNotSupported is returned when Stop is called on a Talos-on-Kubernetes provisioner.
	ErrStopNotSupported = errors.New(
		"stop not supported for Talos-on-Kubernetes: delete the cluster instead",
	)
	// ErrInvalidPort is returned when a port number is outside the valid TCP range [1, 65535].
	ErrInvalidPort = errors.New("port out of valid range [1, 65535]")
	// ErrReplacementServerNotCreated is returned when a rolling node replacement
	// fails to produce a new server.
	ErrReplacementServerNotCreated = errors.New("no replacement server was created")
	// ErrInsufficientControlPlanesForRoll is returned when a rolling control-plane
	// replacement is attempted with too few control planes currently present to
	// preserve etcd quorum.
	ErrInsufficientControlPlanesForRoll = errors.New(
		"too few control planes present to roll without losing etcd quorum",
	)
	// ErrNodeNameTooLong is returned when a generated Hetzner node name exceeds the
	// 63-character DNS-1123 label limit. The node name doubles as the Talos
	// hostname and the Kubernetes node name the Hetzner CCM matches against, so an
	// over-long name would fail config apply or register a node the CCM cannot
	// match. The cluster name is capped at 63, but appending "-<role>-<index>" can
	// still overflow the limit.
	ErrNodeNameTooLong = errors.New("generated node name exceeds maximum length")
)
