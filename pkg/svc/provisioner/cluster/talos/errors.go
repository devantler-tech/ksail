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
)
