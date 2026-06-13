package v1alpha1_test

import (
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
	assert.False(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[v1alpha1.GitOpsEngine]()))
	assert.False(t, v1alpha1.IsToggleEnumType(reflect.TypeFor[string]()))
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
