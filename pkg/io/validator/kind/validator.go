package kind

import (
	"github.com/devantler-tech/ksail-go/pkg/io/validator"
	"github.com/devantler-tech/ksail-go/pkg/io/validator/metadata"
	kindapi "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Validator validates Kind cluster configurations using upstream Kind APIs.
type Validator struct{}

// NewValidator creates a new Kind configuration validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate performs validation on a loaded Kind cluster configuration.
func (v *Validator) Validate(config *kindapi.Cluster) *validator.ValidationResult {
	result := validator.NewValidationResult("kind.yaml")

	// Handle nil config
	if config == nil {
		result.AddError(validator.ValidationError{
			Field:         "config",
			Message:       "configuration is nil",
			FixSuggestion: "Provide a valid Kind cluster configuration",
		})

		return result
	}

	// Validate required metadata fields
	metadata.ValidateMetadata(
		config.Kind,
		config.APIVersion,
		"Cluster",
		"kind.x-k8s.io/v1alpha4",
		result,
	)

	return result
}
