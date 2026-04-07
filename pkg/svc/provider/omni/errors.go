package omni

import "errors"

// Sentinel errors for Omni-specific failure modes.
var (
	// ErrEndpointRequired is returned when the Omni API endpoint is not configured.
	ErrEndpointRequired = errors.New("omni endpoint is required")
	// ErrServiceAccountKeyRequired is returned when the Omni service account key is not set.
	ErrServiceAccountKeyRequired = errors.New("omni service account key is not set")
	// ErrTemplateReaderRequired is returned when templateReader is nil.
	ErrTemplateReaderRequired = errors.New("templateReader must not be nil")
	// ErrNoTalosVersions is returned when no Talos versions are available in Omni.
	ErrNoTalosVersions = errors.New("no Talos versions available in Omni")
	// ErrInsufficientAvailableMachines is returned when Omni does not have enough
	// available (unallocated) machines to satisfy the requested node count.
	ErrInsufficientAvailableMachines = errors.New(
		"not enough available machines in Omni for the requested node count",
	)
	// ErrNegativeMachineCount is returned when a negative machine count is passed
	// to ListAvailableMachines.
	ErrNegativeMachineCount = errors.New("machine count must not be negative")
)
