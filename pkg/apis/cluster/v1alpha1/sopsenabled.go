package v1alpha1

// SOPSEnabled is the tri-state toggle enum for spec.cluster.sops.enabled. It
// replaces the previous *bool, converging SOPS with the other tri-state toggle
// enums (CSI, CDI, MetricsServer, LoadBalancer) while preserving the original
// three states the *bool encoded:
//
//   - Default ("" / unset) = auto-detect: create the SOPS Age secret only when an
//     Age key is available (mirrors the old nil pointer).
//   - Enabled = require: create the secret and error if no key is found (old *true).
//   - Disabled = skip secret creation entirely (old *false).
//
// A YAML/JSON boolean is still accepted on load (true -> Enabled, false ->
// Disabled) via the toggle bool-alias decode hook, so existing ksail.yaml files
// that wrote `sops.enabled: true|false` keep working unchanged.
type SOPSEnabled string

const (
	// SOPSEnabledDefault auto-detects: the SOPS Age secret is created only when an
	// Age key is found via env var or key file. This is the default (unset) state.
	SOPSEnabledDefault SOPSEnabled = "Default"
	// SOPSEnabledEnabled requires an Age key: the secret is created and an error is
	// returned if no key is found.
	SOPSEnabledEnabled SOPSEnabled = "Enabled"
	// SOPSEnabledDisabled disables SOPS Age secret creation entirely.
	SOPSEnabledDisabled SOPSEnabled = "Disabled"
)

// ValidSOPSEnableds returns supported SOPS enabled values.
func ValidSOPSEnableds() []SOPSEnabled {
	return []SOPSEnabled{SOPSEnabledDefault, SOPSEnabledEnabled, SOPSEnabledDisabled}
}

// Set for SOPSEnabled (pflag.Value interface). Accepts the legacy boolean
// spelling (true/false) as a deprecation alias for Enabled/Disabled.
func (s *SOPSEnabled) Set(value string) error {
	return setToggleEnum(s, value, ValidSOPSEnableds(), ErrInvalidSOPSEnabled)
}

// String returns the string representation of the SOPSEnabled.
func (s *SOPSEnabled) String() string {
	return string(*s)
}

// Type returns the type of the SOPSEnabled.
func (s *SOPSEnabled) Type() string {
	return "SOPSEnabled"
}

// Default returns the default value for SOPSEnabled (Default = auto-detect).
func (s *SOPSEnabled) Default() any {
	return SOPSEnabledDefault
}

// ValidValues returns all valid SOPSEnabled values as strings.
func (s *SOPSEnabled) ValidValues() []string {
	return validValueStrings(ValidSOPSEnableds())
}

// IsExplicitlyEnabled reports whether SOPS is explicitly required (the old
// *bool == &true). Auto-detect (Default) and Disabled both return false.
func (s *SOPSEnabled) IsExplicitlyEnabled() bool {
	return *s == SOPSEnabledEnabled
}

// IsExplicitlyDisabled reports whether SOPS is explicitly disabled (the old
// *bool == &false). Auto-detect (Default) and Enabled both return false.
func (s *SOPSEnabled) IsExplicitlyDisabled() bool {
	return *s == SOPSEnabledDisabled
}

// IsAutoDetect reports whether SOPS is in auto-detect mode (the old nil *bool):
// create the secret only when an Age key is found. The empty (unset) value is
// treated as auto-detect.
func (s *SOPSEnabled) IsAutoDetect() bool {
	return *s == SOPSEnabledDefault || *s == ""
}

// UnmarshalJSON accepts the legacy boolean spelling (true -> Enabled, false ->
// Disabled) that older ksail versions persisted to cluster state
// (~/.ksail/clusters/<name>/spec.json, when this field was a *bool) and that REST
// API / operator clients may still send, in addition to the current string form.
// See unmarshalToggleEnumJSON: this keeps pre-migration spec.json and payloads
// loadable after an upgrade (the YAML path is handled by the decode hook).
func (s *SOPSEnabled) UnmarshalJSON(data []byte) error {
	return unmarshalToggleEnumJSON(s, data, ValidSOPSEnableds(), ErrInvalidSOPSEnabled)
}
