package v1alpha1 //nolint:dupl // enum types follow a consistent pattern by design

import (
	"fmt"
	"strings"
)

// CDI defines the CDI (Container Device Interface) options for a KSail cluster.
type CDI string

const (
	// CDIDefault relies on the distribution's default behavior for CDI.
	CDIDefault CDI = "Default"
	// CDIEnabled ensures CDI is enabled in the container runtime.
	CDIEnabled CDI = "Enabled"
	// CDIDisabled ensures CDI is disabled in the container runtime.
	CDIDisabled CDI = "Disabled"
)

// Set for CDI (pflag.Value interface).
func (c *CDI) Set(value string) error {
	for _, cdi := range ValidCDIs() {
		if strings.EqualFold(value, string(cdi)) {
			*c = cdi

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s, %s)",
		ErrInvalidCDI, value, CDIDefault, CDIEnabled, CDIDisabled)
}

// String returns the string representation of the CDI.
func (c *CDI) String() string {
	return string(*c)
}

// Type returns the type of the CDI.
func (c *CDI) Type() string {
	return "CDI"
}

// Default returns the default value for CDI (Default).
func (c *CDI) Default() any {
	return CDIDefault
}

// ValidValues returns all valid CDI values as strings.
func (c *CDI) ValidValues() []string {
	return []string{string(CDIDefault), string(CDIEnabled), string(CDIDisabled)}
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that enable CDI by default (e.g. Talos 1.13+),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (c *CDI) EffectiveValue(distribution Distribution, _ Provider) CDI {
	if *c != CDIDefault && *c != "" {
		return *c
	}

	if distribution.ProvidesCDIByDefault() {
		return CDIEnabled
	}

	return CDIDisabled
}
