package v1alpha1

// ImageVerification defines image verification options for supported distributions.
// Talos uses a native ImageVerificationConfig document; Kind uses containerd config patches.
type ImageVerification string

const (
	// ImageVerificationEnabled enables image verification scaffolding.
	ImageVerificationEnabled ImageVerification = "Enabled"
	// ImageVerificationDisabled disables image verification scaffolding.
	ImageVerificationDisabled ImageVerification = "Disabled"
)

// ValidImageVerifications returns supported image verification values.
func ValidImageVerifications() []ImageVerification {
	return []ImageVerification{
		ImageVerificationEnabled,
		ImageVerificationDisabled,
	}
}

// Set for ImageVerification (pflag.Value interface).
func (iv *ImageVerification) Set(value string) error {
	return setEnum(iv, value, ValidImageVerifications(), ErrInvalidImageVerification)
}

// String returns the string representation of the ImageVerification.
func (iv *ImageVerification) String() string {
	return string(*iv)
}

// Type returns the type of the ImageVerification.
func (iv *ImageVerification) Type() string {
	return "ImageVerification"
}

// Default returns the default value for ImageVerification (Disabled).
func (iv *ImageVerification) Default() any {
	return ImageVerificationDisabled
}

// ValidValues returns all valid ImageVerification values as strings.
func (iv *ImageVerification) ValidValues() []string {
	return validValueStrings(ValidImageVerifications())
}
