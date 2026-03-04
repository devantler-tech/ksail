package v1alpha1

import (
	"fmt"
	"strings"
)

// CNI defines the CNI options for a KSail cluster.
type CNI string

const (
	// CNIDefault is the default CNI.
	CNIDefault CNI = "Default"
	// CNICilium is the Cilium CNI.
	CNICilium CNI = "Cilium"
	// CNICalico is the Calico CNI.
	CNICalico CNI = "Calico"
)

// Set for CNI (pflag.Value interface).
func (c *CNI) Set(value string) error {
	for _, cni := range ValidCNIs() {
		if strings.EqualFold(value, string(cni)) {
			*c = cni

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s, %s)",
		ErrInvalidCNI, value, CNIDefault, CNICilium, CNICalico)
}

// String returns the string representation of the CNI.
func (c *CNI) String() string {
	return string(*c)
}

// Type returns the type of the CNI.
func (c *CNI) Type() string {
	return "CNI"
}

// Default returns the default value for CNI (Default).
func (c *CNI) Default() any {
	return CNIDefault
}

// ValidValues returns all valid CNI values as strings.
func (c *CNI) ValidValues() []string {
	return []string{string(CNIDefault), string(CNICilium), string(CNICalico)}
}
