package v1alpha1

import (
	"reflect"
	"slices"
)

// Toggle string values shared by every Enabled/Disabled enum in this package.
// They are restated as untyped string constants (rather than referencing each
// enum's own typed const) so the bool-alias coercion below has one canonical
// target independent of which toggle type a field uses.
const (
	// ToggleEnabled is the canonical "on" value for every toggle enum.
	ToggleEnabled = "Enabled"
	// ToggleDisabled is the canonical "off" value for every toggle enum.
	ToggleDisabled = "Disabled"
)

// BoolToToggleValue maps a YAML/JSON boolean onto the canonical toggle enum
// value: true -> "Enabled", false -> "Disabled". The config loader uses it to
// accept booleans as a non-breaking alias for the Enabled/Disabled string enum
// fields (the long-standing string spelling keeps working unchanged); a
// mapstructure decode hook coerces a bool landing in a toggle-enum-typed field
// through this function before the enum's own validation runs.
func BoolToToggleValue(enabled bool) string {
	if enabled {
		return ToggleEnabled
	}

	return ToggleDisabled
}

// ToggleEnumTypes returns the reflect.Type of every on/off enum that accepts a
// boolean alias at config load (see BoolToToggleValue). It is the single source
// of truth for which fields the bool-coercion decode hook applies to: both the
// bi-state {Enabled,Disabled} family (CertManager, ImageVerification,
// IngressFirewall, PodAutoscalerHorizontal, PodAutoscalerVertical) and the
// tri-state {Default,Enabled,Disabled} family (CSI, CDI, MetricsServer,
// LoadBalancer) accept booleans, since "Default" remains expressible as the
// string value. Adding a new toggle enum requires adding it here; the drift
// guard in toggle_test.go fails otherwise.
func ToggleEnumTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[CertManager](),
		reflect.TypeFor[ImageVerification](),
		reflect.TypeFor[IngressFirewall](),
		reflect.TypeFor[PodAutoscalerHorizontal](),
		reflect.TypeFor[PodAutoscalerVertical](),
		reflect.TypeFor[CSI](),
		reflect.TypeFor[CDI](),
		reflect.TypeFor[MetricsServer](),
		reflect.TypeFor[LoadBalancer](),
	}
}

// IsToggleEnumType reports whether t is one of the toggle enum types that
// accept a boolean alias at config load. It lets the decode hook test a target
// field type without re-deriving the type set.
func IsToggleEnumType(t reflect.Type) bool {
	return slices.Contains(ToggleEnumTypes(), t)
}
