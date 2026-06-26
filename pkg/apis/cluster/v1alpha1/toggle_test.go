package v1alpha1_test

import (
	"encoding/json"
	"reflect"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoolToToggleValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, v1alpha1.ToggleEnabled, v1alpha1.BoolToToggleValue(true))
	assert.Equal(t, v1alpha1.ToggleDisabled, v1alpha1.BoolToToggleValue(false))
}

func TestIsToggleEnumType(t *testing.T) {
	t.Parallel()

	assert.True(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[v1alpha1.CertManager]()))
	assert.True(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[v1alpha1.CSI]()))
	assert.True(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[v1alpha1.SOPSEnabled]()))
	assert.True(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[v1alpha1.NodeAutoscalerEnabled]()))
	assert.False(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[v1alpha1.GitOpsEngine]()))
	assert.False(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[string]()))
}

// TestSOPSEnabled_SetAcceptsBoolAlias verifies the SOPSEnabled flag Set accepts the
// legacy boolean spelling as a deprecation alias while still accepting the canonical
// enum spellings.
func TestSOPSEnabled_SetAcceptsBoolAlias(t *testing.T) {
	t.Parallel()

	var sops v1alpha1.SOPSEnabled

	require.NoError(t, sops.Set("true"))
	assert.Equal(t, v1alpha1.SOPSEnabledEnabled, sops)

	require.NoError(t, sops.Set("false"))
	assert.Equal(t, v1alpha1.SOPSEnabledDisabled, sops)

	require.NoError(t, sops.Set("Default"))
	assert.Equal(t, v1alpha1.SOPSEnabledDefault, sops)

	require.Error(t, sops.Set("bogus"))
}

// TestNodeAutoscalerEnabled_SetAcceptsBoolAlias verifies the NodeAutoscalerEnabled
// flag Set accepts the legacy boolean spelling as a deprecation alias.
func TestNodeAutoscalerEnabled_SetAcceptsBoolAlias(t *testing.T) {
	t.Parallel()

	var node v1alpha1.NodeAutoscalerEnabled

	require.NoError(t, node.Set("true"))
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledEnabled, node)
	assert.True(t, node.IsEnabled())

	require.NoError(t, node.Set("Disabled"))
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledDisabled, node)
	assert.False(t, node.IsEnabled())
}

// TestToggleEnumsUnmarshalLegacyBoolJSON guards the dual-unmarshal-path
// backward-compat: sops.enabled (was *bool) and autoscaler.node.enabled (was
// bool) migrated to string toggle enums, but older ksail versions persisted them
// to ~/.ksail/clusters/<name>/spec.json (and accept them over the REST API) as
// JSON booleans. A plain json.Unmarshal of those legacy bool values must still
// load (true -> Enabled, false -> Disabled) so `cluster update`/`info` keep
// working after an upgrade — the YAML decode hook does not run on the json path.
func TestToggleEnumsUnmarshalLegacyBoolJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		jsonData        string
		wantSOPS        v1alpha1.SOPSEnabled
		wantNodeEnabled v1alpha1.NodeAutoscalerEnabled
	}{
		{
			name:            "legacy bool true",
			jsonData:        `{"sops":{"enabled":true},"autoscaler":{"node":{"enabled":true}}}`,
			wantSOPS:        v1alpha1.SOPSEnabledEnabled,
			wantNodeEnabled: v1alpha1.NodeAutoscalerEnabledEnabled,
		},
		{
			name:            "legacy bool false",
			jsonData:        `{"sops":{"enabled":false},"autoscaler":{"node":{"enabled":false}}}`,
			wantSOPS:        v1alpha1.SOPSEnabledDisabled,
			wantNodeEnabled: v1alpha1.NodeAutoscalerEnabledDisabled,
		},
		{
			name:            "current string form",
			jsonData:        `{"sops":{"enabled":"Default"},"autoscaler":{"node":{"enabled":"Enabled"}}}`,
			wantSOPS:        v1alpha1.SOPSEnabledDefault,
			wantNodeEnabled: v1alpha1.NodeAutoscalerEnabledEnabled,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var spec v1alpha1.ClusterSpec

			require.NoError(t, json.Unmarshal([]byte(testCase.jsonData), &spec))
			assert.Equal(t, testCase.wantSOPS, spec.SOPS.Enabled)
			assert.Equal(t, testCase.wantNodeEnabled, spec.Autoscaler.Node.Enabled)
		})
	}
}

// TestToggleEnumUnmarshalJSONNull verifies a JSON null leaves the toggle value at
// its zero value (idiomatic no-op), matching default encoding/json behavior.
func TestToggleEnumUnmarshalJSONNull(t *testing.T) {
	t.Parallel()

	var sops v1alpha1.SOPSEnabled

	require.NoError(t, sops.UnmarshalJSON([]byte("null")))
	assert.Equal(t, v1alpha1.SOPSEnabled(""), sops)

	var node v1alpha1.NodeAutoscalerEnabled

	require.NoError(t, node.UnmarshalJSON([]byte("null")))
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabled(""), node)
}

// TestToggleEnumUnmarshalJSONRejectsInvalid guards that the JSON load path
// validates toggle values like the CLI Set path: an unknown string is rejected
// (not silently stored, which would fail-open at e.g. the SOPS secret boundary),
// while a case-insensitive valid string is normalised to the canonical spelling.
func TestToggleEnumUnmarshalJSONRejectsInvalid(t *testing.T) {
	t.Parallel()

	var sops v1alpha1.SOPSEnabled

	require.Error(t, sops.UnmarshalJSON([]byte(`"Enabld"`)))
	require.Error(t, sops.UnmarshalJSON([]byte(`"garbage"`)))

	// Case-insensitive valid value is accepted and stored canonically.
	require.NoError(t, sops.UnmarshalJSON([]byte(`"enabled"`)))
	assert.Equal(t, v1alpha1.SOPSEnabledEnabled, sops)

	var node v1alpha1.NodeAutoscalerEnabled

	require.Error(t, node.UnmarshalJSON([]byte(`"Enbaled"`)))

	require.NoError(t, node.UnmarshalJSON([]byte(`"disabled"`)))
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledDisabled, node)
}

// enumValuer is the minimal interface every enum implements; ValidValues lets
// the drift guard confirm a toggle type's coerced values are accepted.
type toggleEnumValuer interface {
	ValidValues() []string
}

// TestToggleEnumTypesAcceptCoercedValues asserts every type advertised by
// ToggleEnumTypes accepts the canonical "Enabled"/"Disabled" strings that
// BoolToToggleValue produces. Adding a toggle enum whose on/off spellings differ
// (so a coerced bool would yield an invalid value) fails here.
func TestToggleEnumTypesAcceptCoercedValues(t *testing.T) {
	t.Parallel()

	for _, toggleType := range v1alpha1.ToggleEnumTypes() {
		t.Run(toggleType.Name(), func(t *testing.T) {
			t.Parallel()

			// Construct a pointer to a zero value so we can call the
			// pointer-receiver ValidValues method via the interface.
			ptr := reflect.New(toggleType)

			valuer, ok := ptr.Interface().(toggleEnumValuer)
			require.Truef(t, ok, "%s does not implement ValidValues", toggleType.Name())

			values := valuer.ValidValues()
			name := toggleType.Name()
			assert.Containsf(
				t, values, v1alpha1.ToggleEnabled,
				"%s rejects coerced true value %q", name, v1alpha1.ToggleEnabled,
			)
			assert.Containsf(
				t, values, v1alpha1.ToggleDisabled,
				"%s rejects coerced false value %q", name, v1alpha1.ToggleDisabled,
			)
		})
	}
}

// TestSOPSEnabled_StringAndType pins the pflag.Value surface of SOPSEnabled:
// String round-trips the underlying value verbatim (including the empty zero
// value) and Type reports the stable name pflag prints in `--help`.
func TestSOPSEnabled_StringAndType(t *testing.T) {
	t.Parallel()

	for _, value := range []v1alpha1.SOPSEnabled{
		v1alpha1.SOPSEnabledDefault,
		v1alpha1.SOPSEnabledEnabled,
		v1alpha1.SOPSEnabledDisabled,
		v1alpha1.SOPSEnabled(""),
	} {
		state := value
		assert.Equal(t, string(value), state.String())
	}

	zero := v1alpha1.SOPSEnabled("")
	assert.Equal(t, "SOPSEnabled", zero.Type())
}

// TestSOPSEnabled_StateClassifiers locks the tri-state predicates that stand in
// for the field's previous *bool encoding. The empty (unset) zero value MUST
// classify as auto-detect — it mirrors the old nil pointer, so the SOPS secret
// boundary only requires an Age key when the user explicitly enabled it.
func TestSOPSEnabled_StateClassifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		value           v1alpha1.SOPSEnabled
		wantExplicitOn  bool
		wantExplicitOff bool
		wantAutoDetect  bool
	}{
		{"default is auto-detect", v1alpha1.SOPSEnabledDefault, false, false, true},
		{"empty zero value is auto-detect", v1alpha1.SOPSEnabled(""), false, false, true},
		{"enabled is explicit on", v1alpha1.SOPSEnabledEnabled, true, false, false},
		{"disabled is explicit off", v1alpha1.SOPSEnabledDisabled, false, true, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			state := testCase.value
			assert.Equal(
				t,
				testCase.wantExplicitOn,
				state.IsExplicitlyEnabled(),
				"IsExplicitlyEnabled",
			)
			assert.Equal(
				t,
				testCase.wantExplicitOff,
				state.IsExplicitlyDisabled(),
				"IsExplicitlyDisabled",
			)
			assert.Equal(t, testCase.wantAutoDetect, state.IsAutoDetect(), "IsAutoDetect")
		})
	}
}

// TestNodeAutoscalerEnabled_StringAndType pins the pflag.Value surface of
// NodeAutoscalerEnabled, mirroring TestSOPSEnabled_StringAndType.
func TestNodeAutoscalerEnabled_StringAndType(t *testing.T) {
	t.Parallel()

	for _, value := range []v1alpha1.NodeAutoscalerEnabled{
		v1alpha1.NodeAutoscalerEnabledEnabled,
		v1alpha1.NodeAutoscalerEnabledDisabled,
		v1alpha1.NodeAutoscalerEnabled(""),
	} {
		state := value
		assert.Equal(t, string(value), state.String())
	}

	zero := v1alpha1.NodeAutoscalerEnabled("")
	assert.Equal(t, "NodeAutoscalerEnabled", zero.Type())
}
