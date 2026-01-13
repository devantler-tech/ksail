package provider

import "errors"

// Common errors for provider operations.
var (
	// ErrNoNodes is returned when no nodes are found for a cluster.
	ErrNoNodes = errors.New("no nodes found for cluster")

	// ErrProviderUnavailable is returned when the provider is not available.
	ErrProviderUnavailable = errors.New("provider is not available")

	// ErrUnknownLabelScheme is returned when an unknown label scheme is specified.
	ErrUnknownLabelScheme = errors.New("unknown label scheme")

	// ErrSkipAction is a sentinel error indicating no action is needed for the current item.
	// This is used in iteration callbacks to signal that processing should continue
	// to the next item without waiting for any action.
	ErrSkipAction = errors.New("skip action")
)
