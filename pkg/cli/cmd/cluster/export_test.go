package cluster

import (
	"archive/tar"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v5/pkg/svc/state"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ExportShouldPushOCIArtifact exports ShouldPushOCIArtifact for testing.
func ExportShouldPushOCIArtifact(clusterCfg *v1alpha1.Cluster) bool {
	return setup.ShouldPushOCIArtifact(clusterCfg)
}

// ExportSetupK3dCSI exports setupK3dCSI for testing.
func ExportSetupK3dCSI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	setupK3dCSI(clusterCfg, k3dConfig)
}

// ExportResolveClusterNameFromContext exports resolveClusterNameFromContext for testing.
func ExportResolveClusterNameFromContext(ctx *localregistry.Context) string {
	return resolveClusterNameFromContext(ctx)
}

// ExportWriteMetadata exports writeMetadata for testing.
func ExportWriteMetadata(metadata *BackupMetadata, path string) error {
	return writeMetadata(metadata, path)
}

// ExportCreateTarball exports createTarball for testing.
func ExportCreateTarball(sourceDir, targetPath string, compressionLevel int) error {
	return createTarball(sourceDir, targetPath, compressionLevel)
}

// ExportCountYAMLDocuments exports countYAMLDocuments for testing.
func ExportCountYAMLDocuments(content string) int {
	return countYAMLDocuments(content)
}

// ExportFilterExcludedTypes exports filterExcludedTypes for testing.
func ExportFilterExcludedTypes(resourceTypes, excludeTypes []string) []string {
	return filterExcludedTypes(resourceTypes, excludeTypes)
}

// ExportExtractBackupArchive exports extractBackupArchive for testing.
func ExportExtractBackupArchive(inputPath string) (string, *BackupMetadata, error) {
	return extractBackupArchive(inputPath)
}

// ExportSanitizeYAMLOutput exports sanitizeYAMLOutput for testing.
func ExportSanitizeYAMLOutput(output string) (string, error) {
	return sanitizeYAMLOutput(output)
}

// ExportDirPerm exports dirPerm for testing.
const ExportDirPerm = dirPerm

// ExportFilePerm exports filePerm for testing.
const ExportFilePerm = filePerm

// ExportValidateTarEntry exports validateTarEntry for testing.
func ExportValidateTarEntry(header *tar.Header, destDir string) (string, error) {
	return validateTarEntry(header, destDir)
}

// ExportAllLinesContain exports allLinesContain for testing.
func ExportAllLinesContain(output, substr string) bool {
	return allLinesContain(output, substr)
}

// ExportDeriveBackupName exports deriveBackupName for testing.
func ExportDeriveBackupName(inputPath string) string {
	return deriveBackupName(inputPath)
}

// ExportAddLabelsToDocument exports addLabelsToDocument for testing.
func ExportAddLabelsToDocument(doc, backupName, restoreName string) (string, error) {
	return addLabelsToDocument(doc, backupName, restoreName)
}

// ExportSplitYAMLDocuments exports splitYAMLDocuments for testing.
func ExportSplitYAMLDocuments(content string) []string {
	return splitYAMLDocuments(content)
}

// ExportInjectRestoreLabels exports injectRestoreLabels for testing.
func ExportInjectRestoreLabels(filePath, backupName, restoreName string) (string, error) {
	return injectRestoreLabels(filePath, backupName, restoreName)
}

// ExportResolveForce exports resolveForce for testing.
func ExportResolveForce(viperForce bool, yesFlag *pflag.Flag) bool {
	return resolveForce(viperForce, yesFlag)
}

// ExportDisplayChangesSummary exports displayChangesSummary for testing.
func ExportDisplayChangesSummary(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	displayChangesSummary(cmd, diff)
}

// ExportFormatDiffTable exports formatDiffTable for benchmarking.
func ExportFormatDiffTable(diff *clusterupdate.UpdateResult, totalChanges int) string {
	return formatDiffTable(diff, totalChanges)
}

// ExportFormatClusterWithTTL exports formatClusterWithTTL for testing.
func ExportFormatClusterWithTTL(name string, ttl *state.TTLInfo) string {
	return formatClusterWithTTL(name, ttl)
}

// ExportFormatRemainingDuration exports formatRemainingDuration for testing.
func ExportFormatRemainingDuration(d time.Duration) string {
	return formatRemainingDuration(d)
}

// ExportMaybeWaitForTTL exports maybeWaitForTTL for testing.
func ExportMaybeWaitForTTL(cmd *cobra.Command, clusterName string, clusterCfg *v1alpha1.Cluster) error {
	return maybeWaitForTTL(cmd, clusterName, clusterCfg)
}
