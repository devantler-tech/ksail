package hetzner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// CheckServerAvailabilityWithRetry calls CheckServerAvailability up to maxAttempts
// times, waiting with exponential backoff between attempts whenever the requested
// server types are temporarily unavailable in all configured locations. This gives
// transient Hetzner capacity shortages a chance to clear before a create or
// scale-up operation aborts.
//
// Permanent failures (server type not found, provider unavailable) are returned
// immediately without retrying. Progress is reported to logWriter when non-nil.
func (p *Provider) CheckServerAvailabilityWithRetry(
	ctx context.Context,
	serverTypes []string,
	primaryLocation string,
	fallbackLocations []string,
	maxAttempts int,
	logWriter io.Writer,
) error {
	return p.checkServerAvailabilityWithRetry(
		ctx,
		serverTypes,
		primaryLocation,
		fallbackLocations,
		maxAttempts,
		logWriter,
		p.calculateRetryDelay,
	)
}

// checkServerAvailabilityWithRetry implements CheckServerAvailabilityWithRetry with
// an injectable delay function so tests can avoid real backoff sleeps.
func (p *Provider) checkServerAvailabilityWithRetry(
	ctx context.Context,
	serverTypes []string,
	primaryLocation string,
	fallbackLocations []string,
	maxAttempts int,
	logWriter io.Writer,
	delayFunc func(int) time.Duration,
) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = p.CheckServerAvailability(ctx, serverTypes, primaryLocation, fallbackLocations)
		if lastErr == nil {
			if attempt > 1 {
				p.logRetryf(
					logWriter,
					"  ✓ Server type(s) became available after %d attempt(s)\n",
					attempt,
				)
			}

			return nil
		}

		// Permanent failures (e.g. server type not found) won't resolve with retries.
		if !isRetryableAvailabilityError(lastErr) {
			return lastErr
		}

		if attempt == maxAttempts {
			break
		}

		waitErr := p.waitForAvailabilityRetry(
			ctx,
			logWriter,
			attempt,
			maxAttempts,
			lastErr,
			delayFunc,
		)
		if waitErr != nil {
			return waitErr
		}
	}

	return lastErr
}

// waitForAvailabilityRetry logs the retry and waits for the backoff delay,
// returning early if the context is cancelled.
func (p *Provider) waitForAvailabilityRetry(
	ctx context.Context,
	logWriter io.Writer,
	attempt int,
	maxAttempts int,
	err error,
	delayFunc func(int) time.Duration,
) error {
	delay := delayFunc(attempt)

	p.logRetryf(
		logWriter,
		"  ⚠ Server type(s) unavailable (attempt %d/%d): %v. Retrying in %v...\n",
		attempt,
		maxAttempts,
		err,
		delay,
	)

	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during availability retry: %w", ctx.Err())
	case <-time.After(delay):
		return nil
	}
}

// isRetryableAvailabilityError reports whether an availability-check failure is
// transient and worth retrying. Temporary unavailability of a server type in all
// configured locations and retryable Hetzner API errors qualify; permanent
// failures (server type not found, provider unavailable, invalid input) do not.
func isRetryableAvailabilityError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, ErrServerTypeUnavailable) {
		return true
	}

	return IsRetryableHetznerError(err)
}

// CheckServerAvailability verifies that every requested server type is available
// in at least one of the configured locations (primary + fallbacks).
// It returns an error on the first server type that cannot be satisfied.
// Call this before creating infrastructure resources to fail fast.
func (p *Provider) CheckServerAvailability(
	ctx context.Context,
	serverTypes []string,
	primaryLocation string,
	fallbackLocations []string,
) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	uniqueTypes := deduplicateServerTypes(serverTypes)
	locations := buildLocationList(primaryLocation, fallbackLocations)

	for _, serverTypeName := range uniqueTypes {
		err := p.checkSingleServerType(ctx, serverTypeName, locations)
		if err != nil {
			return err
		}
	}

	return nil
}

// checkSingleServerType verifies that serverTypeName is available in at least one
// of the given locations.
func (p *Provider) checkSingleServerType(
	ctx context.Context,
	serverTypeName string,
	locations []string,
) error {
	serverType, _, err := p.client.ServerType.GetByName(ctx, serverTypeName)
	if err != nil {
		return fmt.Errorf("looking up server type %q: %w", serverTypeName, err)
	}

	if serverType == nil {
		return fmt.Errorf("%w: %q", ErrServerTypeNotFound, serverTypeName)
	}

	available := availableLocations(serverType, locations)
	if len(available) == 0 {
		return fmt.Errorf(
			"%w: %q is not available in %s",
			ErrServerTypeUnavailable,
			serverTypeName,
			strings.Join(locations, ", "),
		)
	}

	return nil
}

// availableLocations returns the subset of candidateLocations where the
// server type reports Available == true.
func availableLocations(
	serverType *hcloud.ServerType,
	candidateLocations []string,
) []string {
	var result []string

	for _, candidate := range candidateLocations {
		for _, stLoc := range serverType.Locations {
			if stLoc.Location != nil &&
				stLoc.Location.Name == candidate &&
				stLoc.Available {
				result = append(result, candidate)

				break
			}
		}
	}

	return result
}

// deduplicateServerTypes returns a deduplicated slice preserving order.
// Empty and whitespace-only entries are skipped.
func deduplicateServerTypes(types []string) []string {
	seen := make(map[string]struct{}, len(types))
	result := make([]string, 0, len(types))

	for _, serverType := range types {
		trimmed := strings.TrimSpace(serverType)
		if trimmed == "" {
			continue
		}

		if _, ok := seen[trimmed]; ok {
			continue
		}

		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	return result
}

// buildLocationList constructs the ordered list of locations to check:
// primary first, then fallbacks. Empty strings are skipped and
// duplicates are removed while preserving order.
func buildLocationList(primary string, fallbacks []string) []string {
	seen := make(map[string]struct{}, 1+len(fallbacks))
	locations := make([]string, 0, 1+len(fallbacks))

	for _, loc := range append([]string{primary}, fallbacks...) {
		trimmed := strings.TrimSpace(loc)
		if trimmed == "" {
			continue
		}

		if _, ok := seen[trimmed]; ok {
			continue
		}

		seen[trimmed] = struct{}{}
		locations = append(locations, trimmed)
	}

	return locations
}
