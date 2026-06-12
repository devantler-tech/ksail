package v1alpha1

// CertManager defines the cert-manager options for a KSail cluster.
type CertManager string

const (
	// CertManagerEnabled ensures cert-manager is installed.
	CertManagerEnabled CertManager = "Enabled"
	// CertManagerDisabled ensures cert-manager is not installed.
	CertManagerDisabled CertManager = "Disabled"
)

// ValidCertManagers returns supported cert-manager values.
func ValidCertManagers() []CertManager {
	return []CertManager{
		CertManagerEnabled,
		CertManagerDisabled,
	}
}

// Set for CertManager (pflag.Value interface).
func (c *CertManager) Set(value string) error {
	return setEnum(c, value, ValidCertManagers(), ErrInvalidCertManager)
}

// String returns the string representation of the CertManager.
func (c *CertManager) String() string {
	return string(*c)
}

// Type returns the type of the CertManager.
func (c *CertManager) Type() string {
	return "CertManager"
}

// Default returns the default value for CertManager (Disabled).
func (c *CertManager) Default() any {
	return CertManagerDisabled
}

// ValidValues returns all valid CertManager values as strings.
func (c *CertManager) ValidValues() []string {
	return validValueStrings(ValidCertManagers())
}
