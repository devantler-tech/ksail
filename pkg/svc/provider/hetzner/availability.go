package hetzner

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

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
func deduplicateServerTypes(types []string) []string {
	seen := make(map[string]struct{}, len(types))
	result := make([]string, 0, len(types))

	for _, serverType := range types {
		if _, ok := seen[serverType]; ok {
			continue
		}

		seen[serverType] = struct{}{}
		result = append(result, serverType)
	}

	return result
}

// buildLocationList constructs the ordered list of locations to check:
// primary first, then fallbacks.
func buildLocationList(primary string, fallbacks []string) []string {
	locations := make([]string, 0, 1+len(fallbacks))
	locations = append(locations, primary)
	locations = append(locations, fallbacks...)

	return locations
}
