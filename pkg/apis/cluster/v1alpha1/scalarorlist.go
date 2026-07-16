package v1alpha1

// ScalarOrList is implemented by slice-typed configuration values that also
// accept a single scalar element of their element type. Such a field is exposed
// in the JSON schema as a oneOf union (see schemas/gen_schema.go), and the docs
// field-table generator renders it as "<element> | []<element>" instead of the
// array-only "[]<element>" so the scalar form is documented too.
//
// A new union type only has to implement this marker to have the docs table
// document both shapes automatically.
//
// +kubebuilder:object:generate=false
type ScalarOrList interface {
	// AcceptsScalarOrList reports that the value accepts either a single scalar
	// element or a list of them. It always returns true and exists only as a
	// marker for the docs generator.
	AcceptsScalarOrList() bool
}
