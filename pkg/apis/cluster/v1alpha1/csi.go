package v1alpha1

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

// ValidCSIs returns supported CSI values.
func ValidCSIs() []CSI {
	return []CSI{CSIDefault, CSIEnabled, CSIDisabled}
}

// Set for CSI (pflag.Value interface).
func (c *CSI) Set(value string) error {
	return setEnum(c, value, ValidCSIs(), ErrInvalidCSI)
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
	return validValueStrings(ValidCSIs())
}
