package provider

import "errors"

// Common errors for provider operations.
var (
	// ErrNoNodes is returned when no nodes are found for a cluster.
	ErrNoNodes = errors.New("no nodes found for cluster")

	// ErrProviderUnavailable is returned when the provider is not available.
	ErrProviderUnavailable = errors.New("provider is not available")
)
