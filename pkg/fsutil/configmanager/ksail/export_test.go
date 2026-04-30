//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package configmanager

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
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

// MigrateDeprecatedNodeCountsForTest exposes migrateDeprecatedNodeCounts for testing.
var MigrateDeprecatedNodeCountsForTest = migrateDeprecatedNodeCounts

// ExpectedDistributionConfigNameForTest exports expectedDistributionConfigName for testing.
func ExpectedDistributionConfigNameForTest(distribution v1alpha1.Distribution) string {
	return expectedDistributionConfigName(distribution)
}

// DistributionConfigIsOppositeDefaultForTest exports distributionConfigIsOppositeDefault for testing.
func DistributionConfigIsOppositeDefaultForTest(
	current string,
	distribution v1alpha1.Distribution,
) bool {
	return distributionConfigIsOppositeDefault(current, distribution)
}
