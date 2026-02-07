package configmanager

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/io/config-manager/loader"
	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
	ksailvalidator "github.com/devantler-tech/ksail/v5/pkg/io/validator/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
)

// validateConfig runs validation on the loaded configuration.
func (m *ConfigManager) validateConfig() error {
	// Create validator with distribution config for cross-validation
	validatorInstance, err := m.createValidatorForDistribution()
	if err != nil {
		// Distribution config loading failed - propagate the error
		return fmt.Errorf("failed to load distribution config for validation: %w", err)
	}

	result := validatorInstance.Validate(m.Config)

	if !result.Valid {
		errorMessages := loader.FormatValidationErrorsMultiline(result)
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: "%s",
			Args:    []any{errorMessages},
			Writer:  m.Writer,
		})

		m.writeValidationWarnings(result)

		// Return validation summary error instead of full error stack
		return loader.NewValidationSummaryError(len(result.Errors), len(result.Warnings))
	}

	m.writeValidationWarnings(result)

	return nil
}

// writeValidationWarnings outputs all validation warnings to the configured writer.
func (m *ConfigManager) writeValidationWarnings(result *validator.ValidationResult) {
	warnings := loader.FormatValidationWarnings(result)
	for _, warning := range warnings {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: warning,
			Writer:  m.Writer,
		})
	}
}

// createValidatorForDistribution creates a validator with the appropriate distribution config.
// Only loads distribution config when Cilium CNI is requested for validation.
func (m *ConfigManager) createValidatorForDistribution() (*ksailvalidator.Validator, error) {
	// Only load distribution config for Cilium CNI validation
	if m.Config.Spec.Cluster.DistributionConfig == "" ||
		m.Config.Spec.Cluster.CNI != v1alpha1.CNICilium {
		return ksailvalidator.NewValidator(), nil
	}

	return m.createDistributionValidator()
}

// createDistributionValidator creates a validator based on the configured distribution.
func (m *ConfigManager) createDistributionValidator() (*ksailvalidator.Validator, error) {
	switch m.Config.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return m.createKindValidator()
	case v1alpha1.DistributionK3s:
		return m.createK3dValidator()
	case v1alpha1.DistributionTalos:
		return m.createTalosValidator()
	default:
		return ksailvalidator.NewValidator(), nil
	}
}

// createKindValidator loads Kind config and returns a validator.
func (m *ConfigManager) createKindValidator() (*ksailvalidator.Validator, error) {
	kindConfig, err := m.loadKindConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return nil, err
	}

	if kindConfig != nil {
		return ksailvalidator.NewValidatorForKind(kindConfig), nil
	}

	return ksailvalidator.NewValidator(), nil
}

// createK3dValidator loads K3d config and returns a validator.
func (m *ConfigManager) createK3dValidator() (*ksailvalidator.Validator, error) {
	k3dConfig, err := m.loadK3dConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return nil, err
	}

	if k3dConfig != nil {
		return ksailvalidator.NewValidatorForK3d(k3dConfig), nil
	}

	return ksailvalidator.NewValidator(), nil
}

// createTalosValidator loads Talos config and returns a validator.
func (m *ConfigManager) createTalosValidator() (*ksailvalidator.Validator, error) {
	talosConfig, err := m.loadTalosConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return nil, err
	}

	if talosConfig != nil {
		return ksailvalidator.NewValidatorForTalos(talosConfig), nil
	}

	return ksailvalidator.NewValidator(), nil
}

// applyDistributionConfigDefaults sets the distribution config name based on the distribution.
func (m *ConfigManager) applyDistributionConfigDefaults() {
	if m.Config == nil {
		return
	}

	expected := expectedDistributionConfigName(m.Config.Spec.Cluster.Distribution)
	if expected == "" {
		return
	}

	current := strings.TrimSpace(m.Config.Spec.Cluster.DistributionConfig)
	if current == "" ||
		distributionConfigIsOppositeDefault(current, m.Config.Spec.Cluster.Distribution) {
		m.Config.Spec.Cluster.DistributionConfig = expected
	}
}

func expectedDistributionConfigName(distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return "kind.yaml"
	case v1alpha1.DistributionK3s:
		return "k3d.yaml"
	case v1alpha1.DistributionTalos:
		return "talos"
	default:
		return ""
	}
}

func distributionConfigIsOppositeDefault(current string, distribution v1alpha1.Distribution) bool {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return current == expectedDistributionConfigName(v1alpha1.DistributionK3s) ||
			current == expectedDistributionConfigName(v1alpha1.DistributionTalos)
	case v1alpha1.DistributionK3s:
		return current == expectedDistributionConfigName(v1alpha1.DistributionVanilla) ||
			current == expectedDistributionConfigName(v1alpha1.DistributionTalos)
	case v1alpha1.DistributionTalos:
		return current == expectedDistributionConfigName(v1alpha1.DistributionVanilla) ||
			current == expectedDistributionConfigName(v1alpha1.DistributionK3s)
	default:
		return false
	}
}

// isFieldEmpty checks if a field pointer points to an empty/zero value.
func isFieldEmpty(fieldPtr any) bool {
	if fieldPtr == nil {
		return true
	}

	fieldVal := reflect.ValueOf(fieldPtr)
	if fieldVal.Kind() != reflect.Ptr || fieldVal.IsNil() {
		return true
	}

	fieldVal = fieldVal.Elem()

	return fieldVal.IsZero()
}
