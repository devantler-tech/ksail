//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package configmanager

import (
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	mapstructure "github.com/go-viper/mapstructure/v2"
)

// SetFieldValueFromFlagForTest exports setFieldValueFromFlag for testing.
var SetFieldValueFromFlagForTest = setFieldValueFromFlag

// MetaV1DurationDecodeHookForTest exports metav1DurationDecodeHook for testing.
func MetaV1DurationDecodeHookForTest() mapstructure.DecodeHookFuncType {
	return metav1DurationDecodeHook()
}

// ErrUnsupportedFlagFieldTypeForTest exports errUnsupportedFlagFieldType for testing.
var ErrUnsupportedFlagFieldTypeForTest = errUnsupportedFlagFieldType

// ResolveVClusterNameForTest exposes resolveVClusterName for testing.
func (m *ConfigManager) ResolveVClusterNameForTest() string {
	return m.resolveVClusterName()
}

// GetDefaultTalosPatchesForTest exposes getDefaultTalosPatches for testing.
func (m *ConfigManager) GetDefaultTalosPatchesForTest() []Patch {
	return m.getDefaultTalosPatches()
}

// Patch re-exports the Talos Patch type for testing.
type Patch = talosconfigmanager.Patch
