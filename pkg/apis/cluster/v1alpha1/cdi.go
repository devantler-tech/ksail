package v1alpha1

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

// ValidCDIs returns supported CDI values.
func ValidCDIs() []CDI {
	return []CDI{CDIDefault, CDIEnabled, CDIDisabled}
}

// Set for CDI (pflag.Value interface).
func (c *CDI) Set(value string) error {
	return setEnum(c, value, ValidCDIs(), ErrInvalidCDI)
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
	return validValueStrings(ValidCDIs())
}
