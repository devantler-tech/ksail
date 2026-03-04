package v1alpha1

// EnumValuer is implemented by string-based enum types to provide their valid values.
// The schema generator uses this interface to automatically discover enum constraints.
type EnumValuer interface {
	// ValidValues returns all valid string values for this enum type.
	ValidValues() []string
}
