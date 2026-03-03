package v1alpha1

import (
	"fmt"
	"strings"
)

// PlacementGroupStrategy defines the placement group strategy for Hetzner Cloud servers.
type PlacementGroupStrategy string

const (
	// PlacementGroupStrategyNone disables placement group usage.
	// Servers can be placed on any available host, which may result in
	// multiple servers on the same physical host.
	PlacementGroupStrategyNone PlacementGroupStrategy = "None"
	// PlacementGroupStrategySpread ensures servers are distributed across
	// different physical hosts for high availability. Note: Hetzner limits
	// spread groups to 10 servers per datacenter.
	PlacementGroupStrategySpread PlacementGroupStrategy = "Spread"
)

// Set for PlacementGroupStrategy (pflag.Value interface).
func (p *PlacementGroupStrategy) Set(value string) error {
	for _, strategy := range ValidPlacementGroupStrategies() {
		if strings.EqualFold(value, string(strategy)) {
			*p = strategy

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidPlacementGroupStrategy,
		value,
		PlacementGroupStrategyNone,
		PlacementGroupStrategySpread,
	)
}

// String returns the string representation of the PlacementGroupStrategy.
func (p *PlacementGroupStrategy) String() string {
	return string(*p)
}

// Type returns the type of the PlacementGroupStrategy.
func (p *PlacementGroupStrategy) Type() string {
	return "PlacementGroupStrategy"
}

// Default returns the default value for PlacementGroupStrategy (Spread).
func (p *PlacementGroupStrategy) Default() any {
	return PlacementGroupStrategySpread
}

// ValidValues returns all valid PlacementGroupStrategy values as strings.
func (p *PlacementGroupStrategy) ValidValues() []string {
	return []string{string(PlacementGroupStrategyNone), string(PlacementGroupStrategySpread)}
}
