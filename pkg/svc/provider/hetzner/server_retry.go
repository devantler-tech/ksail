package hetzner

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ServerRetryOpts configures retry and fallback behavior for server creation.
type ServerRetryOpts struct {
	// FallbackLocations is a list of alternative locations to try if the primary fails.
	FallbackLocations []string
	// AllowPlacementFallback allows disabling placement group if placement fails.
	AllowPlacementFallback bool
	// LogWriter receives retry progress messages. If nil, no logging is performed.
	LogWriter io.Writer
}

// CreateServerWithRetry creates a server with retry and location fallback support.
// It handles transient Hetzner API errors with exponential backoff and can fallback
// to alternative locations when the primary location has resource constraints.
func (p *Provider) CreateServerWithRetry(
	ctx context.Context,
	opts CreateServerOpts,
	retryOpts ServerRetryOpts,
) (*hcloud.Server, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	// Build list of locations to try: primary + fallbacks
	locations := make([]string, 0, 1+len(retryOpts.FallbackLocations))
	locations = append(locations, opts.Location)
	locations = append(locations, retryOpts.FallbackLocations...)

	var lastErr error

	originalPlacementGroupID := opts.PlacementGroupID

	for locationIdx, location := range locations {
		server, err := p.attemptServerCreationInLocation(
			ctx,
			opts,
			retryOpts,
			location,
			locationIdx,
			originalPlacementGroupID,
		)
		if err == nil {
			return server, nil
		}

		lastErr = err

		// Log fallback if more locations available
		if locationIdx < len(locations)-1 {
			p.logLocationFallback(retryOpts.LogWriter, location, locations[locationIdx+1])
		}
	}

	// All locations exhausted
	return nil, fmt.Errorf("%w: %s (last error: %w)", ErrAllLocationsFailed, opts.Name, lastErr)
}

func (p *Provider) attemptServerCreationInLocation(
	ctx context.Context,
	opts CreateServerOpts,
	retryOpts ServerRetryOpts,
	location string,
	locationIdx int,
	originalPlacementGroupID int64,
) (*hcloud.Server, error) {
	currentOpts := opts
	currentOpts.Location = location
	currentOpts.PlacementGroupID = originalPlacementGroupID
	placementDisabledForLocation := false

	for attempt := 1; attempt <= DefaultMaxServerCreateRetries; attempt++ {
		server, err := p.CreateServer(ctx, currentOpts)
		if err == nil {
			p.logSuccessfulFallback(
				retryOpts.LogWriter,
				opts.Name,
				location,
				locationIdx,
				placementDisabledForLocation,
				currentOpts.PlacementGroupID > 0,
			)

			return server, nil
		}

		// Check for permanent errors
		if IsResourceLimitError(err) {
			return nil, fmt.Errorf("permanent error creating server %s: %w", opts.Name, err)
		}

		// Handle placement errors with fallback
		if shouldDisablePlacement(err, retryOpts, currentOpts.PlacementGroupID) {
			p.logPlacementFallback(retryOpts.LogWriter, opts.Name, location)

			currentOpts.PlacementGroupID = 0
			placementDisabledForLocation = true

			continue
		}

		// Check if we should retry this error
		if !shouldRetryError(err) {
			// Non-retryable error - try next location if available
			return nil, err
		}

		// Wait before next retry
		if attempt < DefaultMaxServerCreateRetries {
			waitErr := p.waitForRetryDelay(
				ctx,
				retryOpts.LogWriter,
				attempt,
				opts.Name,
				location,
				err,
			)
			if waitErr != nil {
				return nil, waitErr
			}
		}
	}

	// All retries exhausted for this location
	return nil, fmt.Errorf("%w in location %s", ErrAllRetriesExhausted, location)
}

func (p *Provider) calculateRetryDelay(attempt int) time.Duration {
	return min(DefaultRetryBaseDelay*time.Duration(1<<(attempt-1)), DefaultRetryMaxDelay)
}

func (p *Provider) logLocationFallback(
	logWriter io.Writer,
	currentLocation string,
	nextLocation string,
) {
	p.logRetryf(
		logWriter,
		"  ⚠ All attempts failed in %s, trying fallback location %s...\n",
		currentLocation,
		nextLocation,
	)
}

func (p *Provider) logPlacementFallback(logWriter io.Writer, serverName string, location string) {
	p.logRetryf(
		logWriter,
		"  ⚠ Placement failed for %s in %s, retrying without placement group...\n",
		serverName,
		location,
	)
}

func (p *Provider) logRetryAttempt(
	logWriter io.Writer,
	attempt int,
	serverName string,
	location string,
	err error,
	delay time.Duration,
) {
	p.logRetryf(
		logWriter,
		"  ⚠ Attempt %d/%d failed for %s in %s: %v. Retrying in %v...\n",
		attempt,
		DefaultMaxServerCreateRetries,
		serverName,
		location,
		err,
		delay,
	)
}

func (p *Provider) logRetryf(w io.Writer, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...)
	}
}

func (p *Provider) logSuccessfulFallback(
	logWriter io.Writer,
	serverName string,
	location string,
	locationIdx int,
	placementDisabled bool,
	hasPlacementGroup bool,
) {
	if locationIdx > 0 || placementDisabled {
		p.logRetryf(
			logWriter,
			"  ✓ Server %s created successfully after fallback (location: %s, placement group: %v)\n",
			serverName,
			location,
			hasPlacementGroup,
		)
	}
}

func (p *Provider) waitForRetryDelay(
	ctx context.Context,
	logWriter io.Writer,
	attempt int,
	serverName string,
	location string,
	err error,
) error {
	delay := p.calculateRetryDelay(attempt)
	p.logRetryAttempt(logWriter, attempt, serverName, location, err, delay)

	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
	case <-time.After(delay):
		return nil
	}
}

// shouldDisablePlacement checks if placement group should be disabled after an error.
func shouldDisablePlacement(
	err error,
	retryOpts ServerRetryOpts,
	placementGroupID int64,
) bool {
	return IsPlacementError(err) &&
		retryOpts.AllowPlacementFallback &&
		placementGroupID > 0
}

// shouldRetryError determines if an error should trigger a retry.
func shouldRetryError(err error) bool {
	return IsRetryableHetznerError(err) || IsPlacementError(err)
}
