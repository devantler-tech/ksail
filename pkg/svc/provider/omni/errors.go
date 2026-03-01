package omni

import "errors"

// Sentinel errors for Omni-specific failure modes.
var (
	// ErrEndpointRequired is returned when the Omni API endpoint is not configured.
	ErrEndpointRequired = errors.New("omni endpoint is required")
	// ErrServiceAccountKeyRequired is returned when the Omni service account key is not set.
	ErrServiceAccountKeyRequired = errors.New("omni service account key is not set")
)
