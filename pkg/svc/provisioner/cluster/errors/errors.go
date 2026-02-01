// Package clustererrors provides common error types for cluster provisioners.
//
// This package defines sentinel errors that are shared across different
// cluster provisioner implementations (Kind, K3d, Talos) to enable
// consistent error handling in command handlers.
package clustererrors

import "errors"

// ErrClusterNotFound is returned when a cluster operation is attempted on a non-existent cluster.
// This error is used by all provisioner implementations (Kind, K3d, Talos) when attempting
// to delete, start, or stop a cluster that does not exist.
var ErrClusterNotFound = errors.New("cluster not found")

// ErrProviderNotSet is returned when an infrastructure provider is required but not configured.
var ErrProviderNotSet = errors.New("infrastructure provider not set")

// ErrNoNodesFound is returned when a cluster has no nodes.
var ErrNoNodesFound = errors.New("no nodes found for cluster")

// ErrNotHetznerProvider is returned when an operation requires a Hetzner provider but a different provider is set.
var ErrNotHetznerProvider = errors.New("infrastructure provider is not a Hetzner provider")

// ErrNoControlPlaneNodes is returned when no control-plane nodes are found.
var ErrNoControlPlaneNodes = errors.New("no control-plane nodes found for cluster")

// ErrUnsupportedDistribution is returned when an unsupported distribution is specified.
var ErrUnsupportedDistribution = errors.New("unsupported distribution")

// ErrUnsupportedProvider is returned when an unsupported provider is specified.
var ErrUnsupportedProvider = errors.New("unsupported provider")

// ErrMissingDistributionConfig is returned when no pre-loaded distribution config is provided.
var ErrMissingDistributionConfig = errors.New("missing distribution config")

// ErrRecreationRequired is returned when configuration changes require cluster recreation.
var ErrRecreationRequired = errors.New("cluster recreation required; use delete + create instead")

// ErrConfigNil is returned when a required configuration is nil.
var ErrConfigNil = errors.New("config is nil")

// ErrNoProviderConfigured is returned when no infrastructure provider is configured for an operation.
var ErrNoProviderConfigured = errors.New("no provider configured to get node IPs")

// ErrDockerClientNotConfigured is returned when Docker client is required but not configured.
var ErrDockerClientNotConfigured = errors.New("docker client not configured")

// ErrClusterDoesNotExist is returned when attempting to update a cluster that doesn't exist.
var ErrClusterDoesNotExist = errors.New(
	"cluster does not exist; use 'ksail cluster create' to create a new cluster",
)

// ErrTalosConfigRequired is returned when TalosConfig credentials are required but not available.
// This occurs when attempting to update a Talos cluster without valid PKI credentials.
var ErrTalosConfigRequired = errors.New(
	"TalosConfig required for cluster updates but not available",
)
