package v1alpha1

import (
	"fmt"
	"strings"
)

// CSI defines the CSI options for a KSail cluster.
type CSI string

const (
	// CSIDefault relies on the distribution's default behavior for CSI.
	CSIDefault CSI = "Default"
	// CSIEnabled ensures a CSI driver is installed (local-path-provisioner or Hetzner CSI).
	CSIEnabled CSI = "Enabled"
	// CSIDisabled ensures no CSI driver is installed.
	CSIDisabled CSI = "Disabled"
)

// Set for CSI (pflag.Value interface).
func (c *CSI) Set(value string) error {
	for _, csi := range ValidCSIs() {
		if strings.EqualFold(value, string(csi)) {
			*c = csi

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s, %s)",
		ErrInvalidCSI, value, CSIDefault, CSIEnabled, CSIDisabled)
}

// String returns the string representation of the CSI.
func (c *CSI) String() string {
	return string(*c)
}

// Type returns the type of the CSI.
func (c *CSI) Type() string {
	return "CSI"
}

// Default returns the default value for CSI (Default).
func (c *CSI) Default() any {
	return CSIDefault
}

// ValidValues returns all valid CSI values as strings.
func (c *CSI) ValidValues() []string {
	return []string{string(CSIDefault), string(CSIEnabled), string(CSIDisabled)}
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that bundle a CSI driver (e.g. K3s),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (c *CSI) EffectiveValue(distribution Distribution, provider Provider) CSI {
	if *c != CSIDefault {
		return *c
	}

	if distribution.ProvidesCSIByDefault(provider) {
		return CSIEnabled
	}

	return CSIDisabled
}
