package validator

import (
	"fmt"
)

// ValidationError represents a specific validation failure with detailed context and actionable remediation.
type ValidationError struct {
	// Field is the specific field path that failed validation (e.g., "spec.distribution", "metadata.name")
	Field string `json:"field" yaml:"field"`

	// Message is a human-readable description of the validation error
	Message string `json:"message" yaml:"message"`

	// CurrentValue is the actual value that was found in the configuration
	CurrentValue any `json:"currentValue" yaml:"currentValue"`

	// ExpectedValue is the expected value or constraint that was violated
	ExpectedValue any `json:"expectedValue" yaml:"expectedValue"`

	// FixSuggestion provides actionable guidance on how to fix the error
	FixSuggestion string `json:"fixSuggestion" yaml:"fixSuggestion"`

	// Location provides file and line information where the error occurred
	Location FileLocation `json:"location" yaml:"location"`
}

// Error implements the error interface for ValidationError.
func (ve ValidationError) Error() string {
	if ve.Field != "" {
		return fmt.Sprintf("validation error in field '%s': %s", ve.Field, ve.Message)
	}

	return "validation error: " + ve.Message
}

// ValidationResult contains the overall validation status and collection of validation errors.
type ValidationResult struct {
	// Valid indicates overall validation status (true if no errors)
	Valid bool `json:"valid" yaml:"valid"`

	// Errors contains all validation errors found
	Errors []ValidationError `json:"errors" yaml:"errors"`

	// Warnings contains validation warnings (non-blocking)
	Warnings []ValidationError `json:"warnings" yaml:"warnings"`

	// ConfigFile is the path to the configuration file that was validated
	ConfigFile string `json:"configFile" yaml:"configFile"`
}

// NewValidationResult creates a new ValidationResult for the specified configuration file.
func NewValidationResult(configFile string) *ValidationResult {
	return &ValidationResult{
		Valid:      true,
		Errors:     make([]ValidationError, 0),
		Warnings:   make([]ValidationError, 0),
		ConfigFile: configFile,
	}
}

// HasErrors returns true if the validation result contains any errors.
func (vr *ValidationResult) HasErrors() bool {
	return len(vr.Errors) > 0
}

// HasWarnings returns true if the validation result contains any warnings.
func (vr *ValidationResult) HasWarnings() bool {
	return len(vr.Warnings) > 0
}

// AddError adds a validation error to the result and sets Valid to false.
func (vr *ValidationResult) AddError(err ValidationError) {
	vr.Errors = append(vr.Errors, err)
	vr.Valid = false
}

// AddWarning adds a validation warning to the result without affecting Valid status.
func (vr *ValidationResult) AddWarning(warning ValidationError) {
	vr.Warnings = append(vr.Warnings, warning)
}

// FileLocation provides precise location information for validation errors.
type FileLocation struct {
	// FilePath is the absolute path to the configuration file
	FilePath string `json:"filePath" yaml:"filePath"`

	// Line is the line number where the error occurred (1-based)
	Line int `json:"line" yaml:"line"`

	// Column is the column number where the error occurred (1-based, optional)
	Column int `json:"column" yaml:"column"`
}

// String returns a formatted string representation of the file location.
func (fl FileLocation) String() string {
	if fl.Line > 0 && fl.Column > 0 {
		return fmt.Sprintf("%s:%d:%d", fl.FilePath, fl.Line, fl.Column)
	} else if fl.Line > 0 {
		return fmt.Sprintf("%s:%d", fl.FilePath, fl.Line)
	}

	return fl.FilePath
}
