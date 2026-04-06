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
)
