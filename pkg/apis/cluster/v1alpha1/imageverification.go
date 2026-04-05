package v1alpha1

import (
	"fmt"
	"strings"
)

// ImageVerification defines image verification options for Talos distributions.
type ImageVerification string

const (
	// ImageVerificationEnabled enables image verification scaffolding.
	ImageVerificationEnabled ImageVerification = "Enabled"
	// ImageVerificationDisabled disables image verification scaffolding.
	ImageVerificationDisabled ImageVerification = "Disabled"
)

// Set for ImageVerification (pflag.Value interface).
func (iv *ImageVerification) Set(value string) error {
	for _, v := range ValidImageVerifications() {
		if strings.EqualFold(value, string(v)) {
			*iv = v

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidImageVerification,
		value,
		ImageVerificationEnabled,
		ImageVerificationDisabled,
	)
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
	return []string{string(ImageVerificationEnabled), string(ImageVerificationDisabled)}
}
