package validator

// Validator defines the interface for configuration file validators.
// All implementations must be thread-safe and suitable for concurrent use.
// The type parameter T specifies the specific configuration type this validator handles.
type Validator[T any] interface {
	// Validate performs validation on an already parsed configuration struct.
	// This method focuses on semantic validation of the configuration content.
	//
	// Parameters:
	//   - config: Already parsed configuration struct of type T
	//
	// Returns:
	//   - ValidationResult containing status and any errors found
	//
	// Contract Requirements:
	//   - MUST validate semantic correctness of configuration
	//   - MUST check field constraints and dependencies
	//   - MUST return actionable error messages
	//   - MUST be idempotent (same input = same output)
	//   - MUST handle nil or malformed structs gracefully
	Validate(config T) *ValidationResult
}
