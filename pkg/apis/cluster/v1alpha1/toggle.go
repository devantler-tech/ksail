package v1alpha1

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// IngressFirewall, PodAutoscalerHorizontal, PodAutoscalerVertical,
// NodeAutoscaling, NodeAutoscalerEnabled) and the tri-state
// {Default,Enabled,Disabled} family (CSI, CDI, MetricsServer, LoadBalancer,
// SOPSEnabled) accept booleans, since "Default" remains expressible as the
// string value. Adding a new toggle enum requires adding it here; the drift
// guard in toggle_test.go fails otherwise.
func ToggleEnumTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[CertManager](),
		reflect.TypeFor[ImageVerification](),
		reflect.TypeFor[IngressFirewall](),
		reflect.TypeFor[PodAutoscalerHorizontal](),
		reflect.TypeFor[PodAutoscalerVertical](),
		// NodeAutoscaling is the deprecated alias field spec.cluster.nodeAutoscaling
		// (bi-state); NodeAutoscalerEnabled is the canonical
		// spec.cluster.autoscaler.node.enabled (bool→enum in phase 5). Both accept
		// `: true` as a bool alias consistently with the rest of the toggle family.
		reflect.TypeFor[NodeAutoscaling](),
		reflect.TypeFor[NodeAutoscalerEnabled](),
		reflect.TypeFor[CSI](),
		reflect.TypeFor[CDI](),
		reflect.TypeFor[MetricsServer](),
		reflect.TypeFor[LoadBalancer](),
		reflect.TypeFor[SOPSEnabled](),
	}
}

// unmarshalToggleEnumJSON decodes a toggle enum from JSON, accepting the legacy
// boolean spelling (true -> Enabled, false -> Disabled) that older ksail versions
// persisted to cluster state (~/.ksail/clusters/<name>/spec.json) and that REST
// API / operator clients may still send for fields that were a bool before the
// bool->enum migration. It is the encoding/json counterpart of the config-load
// bool-alias decode hook (BoolToToggleValue): the YAML path coerces via
// mapstructure, but the enum's UnmarshalJSON is what runs on the json.Unmarshal
// read paths (pkg/svc/state, pkg/webui/api), so state files and payloads written
// before the migration stay readable after an upgrade. A JSON string is validated
// against valid (case-insensitively, storing the canonical spelling) exactly like
// the pflag Set path (setToggleEnum -> setEnum), so an unknown value is rejected
// rather than silently stored — matching the pre-migration *bool/bool fields,
// which json.Unmarshal already rejected for a non-bool token.
func unmarshalToggleEnumJSON[T ~string](
	target *T,
	data []byte,
	valid []T,
	errSentinel error,
) error {
	trimmed := bytes.TrimSpace(data)

	// JSON null leaves the value unchanged (matches default encoding/json behavior).
	if string(trimmed) == "null" {
		return nil
	}

	// Legacy boolean spelling: coerce true/false through the shared normaliser.
	// BoolToToggleValue yields a canonical Enabled/Disabled, valid for every toggle.
	var boolValue bool

	err := json.Unmarshal(trimmed, &boolValue)
	if err == nil {
		*target = T(BoolToToggleValue(boolValue))

		return nil
	}

	// Current spelling: a JSON string validated against the enum's valid set.
	var strValue string

	err = json.Unmarshal(trimmed, &strValue)
	if err != nil {
		return fmt.Errorf("unmarshal toggle enum: %w", err)
	}

	return setEnum(target, strValue, valid, errSentinel)
}

// IsToggleEnumType reports whether t is one of the toggle enum types that
// accept a boolean alias at config load. It lets the decode hook test a target
// field type without re-deriving the type set.
func IsToggleEnumType(t reflect.Type) bool {
	return slices.Contains(ToggleEnumTypes(), t)
}
