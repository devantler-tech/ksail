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

// WarnDeprecatedTalosPatchFieldsForTest exposes warnDeprecatedTalosPatchFields for testing.
var WarnDeprecatedTalosPatchFieldsForTest = warnDeprecatedTalosPatchFields

// WarnKubernetesVersionCappedForTest exposes warnKubernetesVersionCapped for testing.
var WarnKubernetesVersionCappedForTest = warnKubernetesVersionCapped

// MigrateDeprecatedNodeCountsForTest exposes migrateDeprecatedNodeCounts for testing.
var MigrateDeprecatedNodeCountsForTest = migrateDeprecatedNodeCounts

// MigrateDeprecatedNodeAutoscalingForTest exposes migrateDeprecatedNodeAutoscaling for testing.
var MigrateDeprecatedNodeAutoscalingForTest = migrateDeprecatedNodeAutoscaling

// MigrateDeprecatedImageVerificationForTest exposes migrateDeprecatedImageVerification for testing.
var MigrateDeprecatedImageVerificationForTest = migrateDeprecatedImageVerification

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

// RemoveWorkerRoleLabelPatchForTest exposes removeWorkerRoleLabelPatch for testing.
func (m *ConfigManager) RemoveWorkerRoleLabelPatchForTest(patchesDir string) {
	m.removeWorkerRoleLabelPatch(patchesDir)
}

// LegacyWorkerRoleLabelPatchYAMLForTest exports the legacy machine.nodeLabels constant for testing.
const LegacyWorkerRoleLabelPatchYAMLForTest = legacyWorkerRoleLabelPatchYAML

// KubeletWorkerRoleLabelPatchYAMLForTest exports the kubelet --node-labels constant for testing.
const KubeletWorkerRoleLabelPatchYAMLForTest = kubeletWorkerRoleLabelPatchYAML

// IngressFirewallPatchesForTest exposes ingressFirewallPatches for testing.
var IngressFirewallPatchesForTest = ingressFirewallPatches

// IngressFirewallPatchPathsForTest exposes ingressFirewallPatchPaths for testing.
var IngressFirewallPatchPathsForTest = ingressFirewallPatchPaths

// IngressFirewallPatchFilesExistForTest exposes ingressFirewallPatchFilesExist for testing.
var IngressFirewallPatchFilesExistForTest = ingressFirewallPatchFilesExist

// KubeletPatchFilesExistForTest exposes kubeletPatchFilesExist for testing.
var KubeletPatchFilesExistForTest = kubeletPatchFilesExist

// ReadEKSConfigMetadataForTest exposes readEKSConfigMetadata for testing.
var ReadEKSConfigMetadataForTest = readEKSConfigMetadata

// ErrIngressFirewallMissingCIDRForTest exposes errIngressFirewallMissingCIDR for testing.
var ErrIngressFirewallMissingCIDRForTest = errIngressFirewallMissingCIDR

// ErrIngressFirewallInvalidCIDRForTest exposes errIngressFirewallInvalidCIDR for testing.
var ErrIngressFirewallInvalidCIDRForTest = errIngressFirewallInvalidCIDR

// ErrIngressFirewallInvalidPortForTest exposes errIngressFirewallInvalidPort for testing.
var ErrIngressFirewallInvalidPortForTest = errIngressFirewallInvalidPort

// ResolveKWOKNameForTest exposes resolveKWOKName for testing.
func (m *ConfigManager) ResolveKWOKNameForTest() string {
	return m.resolveKWOKName()
}

// ResolveEKSNameFromContextForTest exposes resolveEKSNameFromContext for testing.
func (m *ConfigManager) ResolveEKSNameFromContextForTest() string {
	return m.resolveEKSNameFromContext()
}

// ResolveGKENameFromContextForTest exposes resolveGKENameFromContext for testing.
func (m *ConfigManager) ResolveGKENameFromContextForTest() string {
	return m.resolveGKENameFromContext()
}

// ReadGKEConfigSpecForTest exposes readGKEConfigSpec for testing.
var ReadGKEConfigSpecForTest = readGKEConfigSpec

// ResolveAKSNameFromContextForTest exposes resolveAKSNameFromContext for testing.
func (m *ConfigManager) ResolveAKSNameFromContextForTest() string {
	return m.resolveAKSNameFromContext()
}

// ReadAKSConfigSpecForTest exposes readAKSConfigSpec for testing.
var ReadAKSConfigSpecForTest = readAKSConfigSpec
