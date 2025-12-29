package helpers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/devantler-tech/ksail/v5/pkg/io/marshaller"
	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
)

// Configuration validation errors.

// ErrConfigurationValidationFailed is returned when configuration validation fails.
var ErrConfigurationValidationFailed = errors.New("configuration validation failed")

// ValidationSummaryError is an error that contains only a validation summary message.
// This error type is used to provide a concise summary instead of a full error stack.
type ValidationSummaryError struct {
	ErrorCount   int
	WarningCount int
}

// NewValidationSummaryError creates a new ValidationSummaryError.
func NewValidationSummaryError(errorCount, warningCount int) *ValidationSummaryError {
	return &ValidationSummaryError{
		ErrorCount:   errorCount,
		WarningCount: warningCount,
	}
}

// Error implements the error interface, returning a summary message.
func (e *ValidationSummaryError) Error() string {
	if e.ErrorCount > 0 && e.WarningCount > 0 {
		return "validation reported " + strconv.Itoa(e.ErrorCount) +
			" error(s) and " + strconv.Itoa(e.WarningCount) + " warning(s)"
	}

	if e.ErrorCount > 0 {
		return "validation reported " + strconv.Itoa(e.ErrorCount) + " error(s)"
	}

	return "validation reported " + strconv.Itoa(e.WarningCount) + " warning(s)"
}

// Configuration loading operations.

// LoadConfigFromFile loads a configuration from a file with common error handling and path resolution.
// This function eliminates duplication between different config managers.
//
// Parameters:
//   - configPath: The path to the configuration file
//   - createDefault: Function to create a default configuration
//
// Returns the loaded configuration or an error.
func LoadConfigFromFile[T any](
	configPath string,
	createDefault func() T,
) (T, error) {
	// Resolve the config path (traverse up from current dir if relative)
	resolvedPath, err := io.FindFile(configPath)
	if err != nil {
		var zero T

		return zero, fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Check if config file exists
	_, err = os.Stat(resolvedPath)
	if os.IsNotExist(err) {
		// File doesn't exist, return default configuration
		return createDefault(), nil
	}

	// Read file contents safely
	// Since we've resolved the path through traversal, we use the directory containing the file as the base
	cleaned := filepath.Clean(resolvedPath)
	baseDir := filepath.Dir(cleaned)

	data, err := io.ReadFileSafe(baseDir, cleaned)
	if err != nil {
		var zero T

		return zero, fmt.Errorf("failed to read config file %s: %w", cleaned, err)
	}

	// Parse YAML into the default config (which will overwrite defaults with file values)
	config := createDefault()
	yamlMarshaller := marshaller.YAMLMarshaller[T]{}

	err = yamlMarshaller.Unmarshal(data, &config)
	if err != nil {
		var zero T

		return zero, fmt.Errorf("failed to unmarshal config from %s: %w", cleaned, err)
	}

	return config, nil
}

// Validation error formatting helpers.

// FormatValidationErrors formats validation errors into a single-line readable string.
// This function eliminates duplication between different config managers.
func FormatValidationErrors(result *validator.ValidationResult) string {
	if len(result.Errors) == 0 {
		return ""
	}

	var builder strings.Builder

	for i, err := range result.Errors {
		if i > 0 {
			builder.WriteString("; ")
		}

		builder.WriteString(err.Field)
		builder.WriteString(": ")
		builder.WriteString(err.Message)

		if err.FixSuggestion != "" {
			builder.WriteString(" (")
			builder.WriteString(err.FixSuggestion)
			builder.WriteString(")")
		}
	}

	return builder.String()
}

// FormatValidationErrorsMultiline formats validation errors into a multi-line string for CLI display.
// This function provides a standardized way to format validation errors for user-facing output.
// Format (with notify symbol "✗ " indentation applied):
//
//	✗ error: <message>
//	  field: <field>
//	  fix: <fix>
func FormatValidationErrorsMultiline(result *validator.ValidationResult) string {
	if len(result.Errors) == 0 {
		return ""
	}

	var builder strings.Builder

	for i, err := range result.Errors {
		if i > 0 {
			builder.WriteString("\n")
		}

		builder.WriteString("error: ")
		builder.WriteString(err.Message)
		builder.WriteString("\nfield: ")
		builder.WriteString(err.Field)

		if err.FixSuggestion != "" {
			builder.WriteString("\nfix: ")
			builder.WriteString(err.FixSuggestion)
		}

		builder.WriteString("\n")
	}

	return builder.String()
}

// FormatValidationWarnings formats validation warnings for CLI display.
// This function provides a standardized way to format validation warnings.
func FormatValidationWarnings(result *validator.ValidationResult) []string {
	warnings := make([]string, 0)

	for _, warning := range result.Warnings {
		warnings = append(warnings, fmt.Sprintf("Warning - %s: %s", warning.Field, warning.Message))
	}

	return warnings
}

// Configuration validation operations.

// ValidateConfig validates a configuration and returns an error if validation fails.
// This function eliminates duplication between different config managers.
func ValidateConfig[T any](config T, validatorInstance validator.Validator[T]) error {
	result := validatorInstance.Validate(config)
	if !result.Valid {
		return fmt.Errorf(
			"%w: %s",
			ErrConfigurationValidationFailed,
			FormatValidationErrors(result),
		)
	}

	return nil
}

// LoadAndValidateConfig loads a configuration from disk and validates it using the provided validator.
// This helper combines LoadConfigFromFile and ValidateConfig to reduce duplication across config managers.
// It returns the loaded configuration or an error if loading or validation fails.
func LoadAndValidateConfig[T any](
	configPath string,
	createDefault func() T,
	validatorInstance validator.Validator[T],
) (T, error) {
	config, err := LoadConfigFromFile(configPath, createDefault)
	if err != nil {
		var zero T

		return zero, fmt.Errorf("failed to load config: %w", err)
	}

	err = ValidateConfig(config, validatorInstance)
	if err != nil {
		var zero T

		return zero, fmt.Errorf("failed to validate config: %w", err)
	}

	return config, nil
}
