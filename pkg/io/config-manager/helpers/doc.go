// Package helpers provides common functionality for config managers to eliminate duplication.
//
// This package contains shared utilities used across different config manager
// implementations, including common loading patterns and helper functions.
//
// Key functionality:
//   - LoadConfigFromFile: Generic file loading with path resolution
//   - LoadAndValidateConfig: Combined loading and validation
//   - ValidateConfig: Configuration validation with standardized error handling
//   - FormatValidationErrors: Error formatting for CLI display
//   - ValidationSummaryError: Concise validation error summaries
package helpers
