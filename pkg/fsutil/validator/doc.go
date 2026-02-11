// Package validator provides interfaces for configuration file validation.
//
// This package defines the Validator interface and common validation types
// used across different configuration validators (Kind, K3d, KSail) for
// ensuring configuration correctness and consistency.
//
// Key functionality:
//   - Validator[T]: Generic interface for configuration validation
//   - ValidationResult: Structured validation results with errors and warnings
//   - ValidationError: Detailed error with field, message, fix suggestions, and location
//   - FileLocation: Precise file location information for errors
//   - ValidateMetadata: Common metadata validation for Kind/APIVersion fields
//
// Subpackages:
//   - k3d: K3d configuration validator
//   - kind: Kind configuration validator
//   - ksail: KSail configuration validator
package validator
