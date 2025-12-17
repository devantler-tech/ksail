package k3d

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/io/validator"
	"github.com/devantler-tech/ksail-go/pkg/io/validator/metadata"
	"github.com/jinzhu/copier"
	k3dconfig "github.com/k3d-io/k3d/v5/pkg/config"
	k3dapi "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
)

// Validator validates K3d cluster configurations using upstream K3d APIs.
type Validator struct{}

// NewValidator creates a new K3d configuration validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate performs validation on a loaded K3d cluster configuration using upstream K3d APIs.
// This validator performs both essential field validation and comprehensive upstream K3d validation.
func (v *Validator) Validate(config *k3dapi.SimpleConfig) *validator.ValidationResult {
	result := validator.NewValidationResult("k3d.yaml")

	// Handle nil config
	if config == nil {
		result.AddError(validator.ValidationError{
			Field:         "config",
			Message:       "configuration is nil",
			FixSuggestion: "Provide a valid K3d cluster configuration",
		})

		return result
	}

	// Validate required metadata fields
	metadata.ValidateMetadata(config.Kind, config.APIVersion, "Simple", "k3d.io/v1alpha5", result)

	// Run comprehensive K3d validation using upstream APIs
	err := v.validateWithUpstreamK3d(config)
	if err != nil {
		result.AddError(validator.ValidationError{
			Field:   "config",
			Message: err.Error(),
			FixSuggestion: "Check the K3d cluster configuration schema at " +
				"https://k3d.io/usage/configfile/ for complete requirements and examples",
		})
	}

	return result
}

// validateWithUpstreamK3d runs comprehensive K3d validation following the same workflow as K3d CLI.
func (v *Validator) validateWithUpstreamK3d(config *k3dapi.SimpleConfig) error {
	// Create a deep copy to avoid modifying the original configuration using marshalling/unmarshalling
	configCopy, err := v.deepCopyConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create deep copy of config: %w", err)
	}

	// Step 1: Process simple config (same as K3d CLI workflow)
	processErr := k3dconfig.ProcessSimpleConfig(configCopy)
	if processErr != nil {
		return fmt.Errorf("failed to process simple configuration: %w", processErr)
	}

	// Step 2: Transform simple config to cluster config (requires a runtime context)
	// Use Docker runtime as default since it's the most common runtime for K3d
	runtime := runtimes.Docker
	ctx := context.Background()

	clusterConfig, transformErr := k3dconfig.TransformSimpleToClusterConfig(
		ctx, runtime, *configCopy, "",
	)
	if transformErr != nil {
		return fmt.Errorf("failed to transform configuration: %w", transformErr)
	}

	// Step 3: Process cluster config
	processedConfig, processClusterErr := k3dconfig.ProcessClusterConfig(*clusterConfig)
	if processClusterErr != nil {
		return fmt.Errorf("failed to process cluster configuration: %w", processClusterErr)
	}

	// Step 4: Run comprehensive K3d validation (same as K3d CLI)
	validateErr := k3dconfig.ValidateClusterConfig(ctx, runtime, *processedConfig)
	if validateErr != nil {
		return fmt.Errorf("K3d configuration validation failed: %w", validateErr)
	}

	return nil
}

// deepCopyConfig creates a deep copy of the K3d simple configuration using the copier library.
// This ensures that upstream validation operations cannot modify the original configuration object.
// Using copier is an alternative to JSON marshalling/unmarshalling for creating deep copies.
func (v *Validator) deepCopyConfig(config *k3dapi.SimpleConfig) (*k3dapi.SimpleConfig, error) {
	var configCopy k3dapi.SimpleConfig

	err := copier.Copy(&configCopy, config)
	if err != nil {
		return nil, fmt.Errorf("failed to deep copy config: %w", err)
	}

	return &configCopy, nil
}
