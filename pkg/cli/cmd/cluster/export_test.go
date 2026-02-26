package cluster

import (
	"archive/tar"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
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
