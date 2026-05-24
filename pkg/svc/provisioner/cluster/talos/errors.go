package talosprovisioner

import "errors"

// Common errors for the Talos provisioner.
var (
	// ErrDockerNotAvailable is returned when Docker is not available.
	ErrDockerNotAvailable = errors.New("docker is not available: ensure Docker is running")
	// ErrClusterAlreadyExists is returned when attempting to create a cluster that already exists.
	ErrClusterAlreadyExists = errors.New("cluster already exists")
	// ErrInvalidPatch is returned when a patch file is invalid.
	ErrInvalidPatch = errors.New("invalid patch file")
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
)
