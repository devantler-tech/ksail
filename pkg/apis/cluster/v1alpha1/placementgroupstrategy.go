package v1alpha1

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

// ValidPlacementGroupStrategies returns supported placement group strategy values.
func ValidPlacementGroupStrategies() []PlacementGroupStrategy {
	return []PlacementGroupStrategy{PlacementGroupStrategyNone, PlacementGroupStrategySpread}
}

// Set for PlacementGroupStrategy (pflag.Value interface).
func (p *PlacementGroupStrategy) Set(value string) error {
	return setEnum(p, value, ValidPlacementGroupStrategies(), ErrInvalidPlacementGroupStrategy)
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
	return validValueStrings(ValidPlacementGroupStrategies())
}
