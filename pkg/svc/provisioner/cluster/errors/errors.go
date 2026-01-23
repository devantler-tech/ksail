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
