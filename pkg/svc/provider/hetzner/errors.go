package hetzner

import (
	"errors"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Sentinel errors for Hetzner-specific failure modes.
var (
	// ErrResourceUnavailable indicates that Hetzner resources are temporarily unavailable.
	ErrResourceUnavailable = errors.New("hetzner resource unavailable")
	// ErrPlacementFailed indicates that server placement constraints could not be satisfied.
	ErrPlacementFailed = errors.New("hetzner placement failed")
	// ErrAllLocationsFailed indicates that server creation failed in all attempted locations.
	ErrAllLocationsFailed = errors.New("server creation failed in all locations")
)

// retryableErrorCodes are Hetzner API error codes that warrant a retry.
// These represent transient conditions that may resolve on subsequent attempts.
//
//nolint:gochecknoglobals // Package-level constant for error code classification
var retryableErrorCodes = []hcloud.ErrorCode{
	hcloud.ErrorCodeResourceUnavailable, // Resource currently unavailable
	hcloud.ErrorCodeConflict,            // Resource changed during request
	hcloud.ErrorCodeTimeout,             // Request timed out
	hcloud.ErrorCodeRateLimitExceeded,   // Rate limit hit
	hcloud.ErrorCodeRobotUnavailable,    // Robot service unavailable
	hcloud.ErrorCodeLocked,              // Resource locked by another action
}

// IsRetryableHetznerError returns true if the error is a transient Hetzner API error
// that may succeed on retry.
func IsRetryableHetznerError(err error) bool {
	if err == nil {
		return false
	}

	return hcloud.IsError(err, retryableErrorCodes...)
}

// IsPlacementError returns true if the error is a Hetzner placement error,
// indicating that server placement constraints (e.g., spread placement group)
// could not be satisfied in the requested location.
func IsPlacementError(err error) bool {
	if err == nil {
		return false
	}

	// Check for explicit placement error code
	if hcloud.IsError(err, hcloud.ErrorCodePlacementError) {
		return true
	}

	// Also check for resource_unavailable during placement (the observed CI error).
	// The error message pattern: "error during placement (resource_unavailable, ...)"
	// indicates this is a placement-related resource constraint.
	var apiErr hcloud.Error
	if errors.As(err, &apiErr) {
		if apiErr.Code == hcloud.ErrorCodeResourceUnavailable {
			// Resource unavailable can be placement-related when creating servers
			// with placement group constraints.
			return true
		}
	}

	return false
}

// IsResourceLimitError returns true if the error indicates a permanent resource limit
// that won't resolve with retries (e.g., quota exceeded, invalid configuration).
func IsResourceLimitError(err error) bool {
	if err == nil {
		return false
	}

	return hcloud.IsError(err,
		hcloud.ErrorCodeResourceLimitExceeded,
		hcloud.ErrorCodeInvalidInput,
		hcloud.ErrorCodeForbidden,
		hcloud.ErrorCodeUnauthorized,
	)
}
