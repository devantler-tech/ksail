package cluster

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

// ExportIsHelmReleaseSecret exports isHelmReleaseSecret for testing.
func ExportIsHelmReleaseSecret(obj *unstructured.Unstructured) bool {
	return isHelmReleaseSecret(obj)
}

// ExportResolveForce exports resolveForce for testing.
func ExportResolveForce(viperForce bool, yesFlag *pflag.Flag) bool {
	return resolveForce(viperForce, yesFlag)
}

// ExportDisplayChangesSummary exports displayChangesSummary for testing.
func ExportDisplayChangesSummary(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	displayChangesSummary(cmd, diff)
}

// ExportDiffToJSON exports diffToJSON for testing.
func ExportDiffToJSON(diff *clusterupdate.UpdateResult) DiffJSONOutput {
	return diffToJSON(diff)
}

// ExportOutputFormatJSON exports outputFormatJSON for testing.
const ExportOutputFormatJSON = outputFormatJSON

// ExportOutputFormatText exports outputFormatText for testing.
const ExportOutputFormatText = outputFormatText

// ExportFormatDiffTable exports formatDiffTable for benchmarking.
func ExportFormatDiffTable(diff *clusterupdate.UpdateResult, totalChanges int) string {
	return formatDiffTable(diff, totalChanges)
}

// ExportStripParenthetical exports stripParenthetical for testing.
func ExportStripParenthetical(s string) string {
	return stripParenthetical(s)
}

// ExportListResult is a test-visible alias for listResult.
type ExportListResult = listResult

// ExportDisplayListResults exports displayListResults for testing.
func ExportDisplayListResults(
	writer io.Writer,
	providers []v1alpha1.Provider,
	results []ExportListResult,
) {
	displayListResults(writer, providers, results)
}

// ExportNewListResult creates a listResult with TTL info for testing.
func ExportNewListResult(
	provider v1alpha1.Provider,
	distribution v1alpha1.Distribution,
	clusterName string,
	ttl *state.TTLInfo,
) ExportListResult {
	return listResult{
		Provider:     provider,
		Distribution: distribution,
		ClusterName:  clusterName,
		TTL:          ttl,
	}
}

// ExportFormatRemainingDuration exports formatRemainingDuration for testing.
func ExportFormatRemainingDuration(d time.Duration) string {
	return formatRemainingDuration(d)
}

// ExportMaybeWaitForTTL exports maybeWaitForTTL for testing.
func ExportMaybeWaitForTTL(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
) error {
	return maybeWaitForTTL(cmd, clusterName, clusterCfg)
}

// ErrMetricsServerDisableUnsupported exports the sentinel error for testing.
var ErrMetricsServerDisableUnsupported = errMetricsServerDisableUnsupported

// ExportHandlerForField reports whether a registered handler exists for the given field name.
func ExportHandlerForField(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, field string) bool {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")
	_, ok := r.handlerForField(field)

	return ok
}

// ExportReconcileMetricsServer exposes reconcileMetricsServer for unit testing.
func ExportReconcileMetricsServer(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	change clusterupdate.Change,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")

	return r.reconcileMetricsServer(context.Background(), change)
}

// ExportReconcileCSI exposes reconcileCSI for unit testing.
func ExportReconcileCSI(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	change clusterupdate.Change,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")

	return r.reconcileCSI(context.Background(), change)
}

// ExportReconcileCertManager exposes reconcileCertManager for unit testing.
func ExportReconcileCertManager(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	change clusterupdate.Change,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")

	return r.reconcileCertManager(context.Background(), change)
}

// ExportReconcilePolicyEngine exposes reconcilePolicyEngine for unit testing.
func ExportReconcilePolicyEngine(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	change clusterupdate.Change,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")

	return r.reconcilePolicyEngine(context.Background(), change)
}

// ExportReconcileGitOpsEngine exposes reconcileGitOpsEngine for unit testing.
func ExportReconcileGitOpsEngine(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	change clusterupdate.Change,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")

	return r.reconcileGitOpsEngine(context.Background(), change)
}

// ExportReconcileComponents exposes reconcileComponents for unit testing.
func ExportReconcileComponents(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	diff *clusterupdate.UpdateResult,
	result *clusterupdate.UpdateResult,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")

	return r.reconcileComponents(context.Background(), diff, result)
}

// ExportMatchesKindPattern exposes matchesKindPattern for unit testing.
func ExportMatchesKindPattern(containerName, clusterName string) bool {
	return matchesKindPattern(containerName, clusterName)
}

// ExportIsNumericString exposes isNumericString for unit testing.
func ExportIsNumericString(s string) bool {
	return isNumericString(s)
}

// ExportIsCloudProviderKindContainer exposes isCloudProviderKindContainer for unit testing.
func ExportIsCloudProviderKindContainer(name string) bool {
	return isCloudProviderKindContainer(name)
}

// ExportIsKindClusterFromNodes exposes isKindClusterFromNodes for unit testing.
func ExportIsKindClusterFromNodes(nodes []string, clusterName string) bool {
	return isKindClusterFromNodes(nodes, clusterName)
}

// ExportApplyDistributionSpecOverrides exposes applyDistributionSpecOverrides for unit testing.
func ExportApplyDistributionSpecOverrides(spec *v1alpha1.ClusterSpec) {
	applyDistributionSpecOverrides(spec)
}

// ExportPickCluster exposes pickCluster for unit testing.
func ExportPickCluster(cmd *cobra.Command, deps SwitchDeps) (string, error) {
	return pickCluster(cmd, deps)
}

// ExportPrintRestoreHeader exposes printRestoreHeader for testing.
// Parameters mirror restoreFlags fields so the unexported struct is not needed.
func ExportPrintRestoreHeader(writer io.Writer, inputPath, policy string, dryRun bool) {
	flags := &restoreFlags{
		inputPath:              inputPath,
		existingResourcePolicy: policy,
		dryRun:                 dryRun,
	}
	printRestoreHeader(writer, flags)
}

// ExportPrintRestoreMetadata exposes printRestoreMetadata for testing.
func ExportPrintRestoreMetadata(writer io.Writer, metadata *BackupMetadata) {
	printRestoreMetadata(writer, metadata)
}

// ExportReadBackupMetadata exposes readBackupMetadata for testing.
func ExportReadBackupMetadata(tmpDir string) (*BackupMetadata, error) {
	return readBackupMetadata(tmpDir)
}

// ExportBackupResourceTypes exposes backupResourceTypes for testing.
func ExportBackupResourceTypes() []string {
	return backupResourceTypes()
}

// ExportEnsureLocalRegistriesReady exports ensureLocalRegistriesReady for testing.
func ExportEnsureLocalRegistriesReady(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) error {
	return ensureLocalRegistriesReady(cmd, ctx, deps, cfgManager, localDeps)
}

// ExportComponentLabel exports componentLabel for testing.
func ExportComponentLabel(value string) string {
	return componentLabel(value)
}

// ClusterWithDistributionInfo is an exported view of clusterWithDistribution for testing.
type ClusterWithDistributionInfo struct {
	Name         string
	Distribution v1alpha1.Distribution
}

// ExportToTalosClusters exports toTalosClusters for testing.
func ExportToTalosClusters(names []string) []ClusterWithDistributionInfo {
	raw := toTalosClusters(names)

	out := make([]ClusterWithDistributionInfo, len(raw))
	for i, r := range raw {
		out[i] = ClusterWithDistributionInfo(r)
	}

	return out
}

// ExportDisplayClusterIdentity exports displayClusterIdentity for testing.
func ExportDisplayClusterIdentity(writer io.Writer, info *clusterdetector.Info) {
	displayClusterIdentity(writer, info)
}

// ExportDisplayTTLInfo exports displayTTLInfo for testing.
func ExportDisplayTTLInfo(writer io.Writer, clusterName string) {
	displayTTLInfo(writer, clusterName)
}

// ExportDisplayComponents exports displayComponents for testing.
func ExportDisplayComponents(writer io.Writer, clusterName string) {
	displayComponents(writer, clusterName)
}

// ExportClassifyRestoreError exports classifyRestoreError for testing.
func ExportClassifyRestoreError(err error, stderr, policy string) error {
	flags := &restoreFlags{
		existingResourcePolicy: policy,
	}

	return classifyRestoreError(err, stderr, flags)
}

// ExportStripDistributionPrefix exports stripDistributionPrefix for testing.
func ExportStripDistributionPrefix(contextName string) string {
	return stripDistributionPrefix(contextName)
}

// ExportIsEmptyYAML exports isEmptyYAML for testing.
func ExportIsEmptyYAML(path string) bool {
	return isEmptyYAML(path)
}

// ExportHasK3sArg exports hasK3sArg for testing.
func ExportHasK3sArg(k3dConfig *v1alpha5.SimpleConfig, flag string) bool {
	return hasK3sArg(k3dConfig, flag)
}

// ExportValidateOutputFormat exports validateOutputFormat for testing.
func ExportValidateOutputFormat(cmd *cobra.Command) error {
	return validateOutputFormat(cmd)
}

// ExportPrepareOutputPath exports prepareOutputPath for testing.
func ExportPrepareOutputPath(outputPath string) (string, error) {
	return prepareOutputPath(outputPath)
}

// ExportPrintBackupSummary exports printBackupSummary for testing.
func ExportPrintBackupSummary(writer io.Writer, outputPath string) {
	printBackupSummary(writer, outputPath)
}

// ExportClusterScopedResourceTypes exports clusterScopedResourceTypes for testing.
func ExportClusterScopedResourceTypes() map[string]bool {
	return clusterScopedResourceTypes()
}

// ExportRemoveAutoGeneratedJobLabels exports removeAutoGeneratedJobLabels for testing.
func ExportRemoveAutoGeneratedJobLabels(obj *unstructured.Unstructured, fields ...string) {
	removeAutoGeneratedJobLabels(obj, fields...)
}

// ExportRemoveServiceClusterIPs exports removeServiceClusterIPs for testing.
func ExportRemoveServiceClusterIPs(obj *unstructured.Unstructured) {
	removeServiceClusterIPs(obj)
}

// ExportCategoryIcon exports categoryIcon for testing.
func ExportCategoryIcon(cat clusterupdate.ChangeCategory) string {
	return categoryIcon(cat)
}

// ExportCommitTarball exports commitTarball for testing.
func ExportCommitTarball(
	tarWriter *tar.Writer,
	gzipWriter *gzip.Writer,
	outFile *os.File,
	tmpPath, targetPath string,
) error {
	return commitTarball(tarWriter, gzipWriter, outFile, tmpPath, targetPath)
}

// ExportAllDistributions exports allDistributions for testing.
func ExportAllDistributions() []v1alpha1.Distribution {
	return allDistributions()
}

// ExportAllProviders exports allProviders for testing.
func ExportAllProviders() []v1alpha1.Provider {
	return allProviders()
}

// ExportGetOutputFormat exports getOutputFormat for testing.
func ExportGetOutputFormat(cmd *cobra.Command) string {
	return getOutputFormat(cmd)
}

// ExportInitFieldSelectors exports InitFieldSelectors for testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var ExportInitFieldSelectors = InitFieldSelectors

// ExportIsClusterContainer exports IsClusterContainer for testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var ExportIsClusterContainer = IsClusterContainer
