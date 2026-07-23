package cluster

import (
	"context"
	"io"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ExportSetEKSIdentityClientFactory replaces SDK client construction for offline lifecycle tests.
func ExportSetEKSIdentityClientFactory(
	factory func(
		context.Context,
		string,
		credentials.AWSResolution,
	) (eksidentity.Client, error),
) func() {
	eksIdentityClientFactoryState.Lock()
	previous := eksIdentityClientFactoryState.factory
	eksIdentityClientFactoryState.factory = factory
	eksIdentityClientFactoryState.Unlock()

	return func() {
		eksIdentityClientFactoryState.Lock()
		eksIdentityClientFactoryState.factory = previous
		eksIdentityClientFactoryState.Unlock()
	}
}

// ExportShouldPushOCIArtifact exports ShouldPushOCIArtifact for testing.
func ExportShouldPushOCIArtifact(clusterCfg *v1alpha1.Cluster) bool {
	return setup.ShouldPushOCIArtifact(clusterCfg)
}

// ExportAWSProviderStatus exports awsProviderStatus for testing.
func ExportAWSProviderStatus(
	ctx context.Context,
	client *eksctlclient.Client,
	clusterName string,
	region string,
	opts ...awsprovider.Option,
) (*provider.ClusterStatus, error) {
	return awsProviderStatus(ctx, client, clusterName, region, opts...)
}

// ExportRestorePersistedAWSOptions exposes persisted AWS option restoration for focused tests.
func ExportRestorePersistedAWSOptions(resolved *lifecycle.ResolvedClusterInfo) error {
	return restorePersistedAWSOptions(resolved)
}

// ErrProviderNotConfigured exports errProviderNotConfigured for testing.
var ErrProviderNotConfigured = errProviderNotConfigured

// ExportSetupK3dCSI exports setupK3dCSI for testing.
func ExportSetupK3dCSI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	setupK3dCSI(clusterCfg, k3dConfig)
}

// ExportSetupK3dCNI exports setupK3dCNI for testing.
func ExportSetupK3dCNI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	setupK3dCNI(clusterCfg, k3dConfig)
}

// ExportResolveClusterNameFromContext exports resolveClusterNameFromContext for testing.
func ExportResolveClusterNameFromContext(ctx *localregistry.Context) string {
	return resolveClusterNameFromContext(ctx)
}

// ExportDefaultProvisionerFactory exports defaultProvisionerFactory for testing.
func ExportDefaultProvisionerFactory(
	ctx *localregistry.Context,
) clusterprovisioner.DefaultFactory {
	return defaultProvisionerFactory(ctx)
}

// ExportCreateAndVerifyProvisioner exports createAndVerifyProvisioner for testing.
func ExportCreateAndVerifyProvisioner(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	clusterName string,
) (clusterprovisioner.Provisioner, error) {
	return createAndVerifyProvisioner(cmd, ctx, clusterName)
}

// ExportPersistRequiredEKSComponentState exports the fail-closed EKS component writer for testing.
func ExportPersistRequiredEKSComponentState(
	ctx *localregistry.Context,
	clusterName string,
) error {
	return persistRequiredEKSComponentState(
		ctx,
		clusterName,
		setup.NeedsLoadBalancerInstall(ctx.ClusterCfg),
	)
}

// ExportFinishCreateWithTTL exports the create finalization ordering for testing.
func ExportFinishCreateWithTTL(requiredStateErr error, waitForTTL func() error) error {
	return finishCreateWithTTL(requiredStateErr, waitForTTL)
}

// ExportResolveClusterContext exports resolveClusterContext for testing.
func ExportResolveClusterContext(kubeconfigPath, clusterName string) (string, error) {
	return resolveClusterContext(kubeconfigPath, clusterName)
}

// ExportEnsureConfiguredContextResolvable exports ensureConfiguredContextResolvable for testing.
func ExportEnsureConfiguredContextResolvable(clusterCfg *v1alpha1.Cluster) error {
	return ensureConfiguredContextResolvable(clusterCfg)
}

// ExportResolveKubeContext exports resolveKubeContext for testing.
func ExportResolveKubeContext(ctx *localregistry.Context) string {
	return resolveKubeContext(ctx)
}

// ExportEnsureClusterManaged exports ensureClusterManaged for testing, driving it from a fixed
// managed-set + completeness pair (instead of the live cross-provider discoverer) so the
// unmanaged-cluster guard can be exercised fully offline.
func ExportEnsureClusterManaged(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
	managed map[string]struct{},
	complete bool,
) error {
	return ensureClusterManaged(
		ctx,
		resolved,
		func(context.Context) (map[string]struct{}, bool) { return managed, complete },
	)
}

// ExportParseEksctlContextTarget exports parseEksctlContextTarget for testing.
func ExportParseEksctlContextTarget(contextName string) (string, string, bool) {
	return parseEksctlContextTarget(contextName)
}

// ExportResolveCreatedContextName exports resolveCreatedContextName for testing.
func ExportResolveCreatedContextName(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	clusterName string,
) string {
	return resolveCreatedContextName(distribution, provider, clusterName)
}

// ExportResolvePostCreateContext exports resolvePostCreateContext for testing.
func ExportResolvePostCreateContext(ctx *localregistry.Context) error {
	return resolvePostCreateContext(ctx)
}

// ExportPrepareEKSCreateConfig exports prepareEKSCreateConfig for testing.
func ExportPrepareEKSCreateConfig(ctx *localregistry.Context) error {
	return prepareEKSCreateConfig(ctx)
}

// ExportApplyClusterNameOverride exports applyClusterNameOverride for testing.
func ExportApplyClusterNameOverride(ctx *localregistry.Context, name string) error {
	return applyClusterNameOverride(ctx, name)
}

// ExportResolveConsent exports resolveConsent for testing.
func ExportResolveConsent(viperForce bool, yesFlag *pflag.Flag) bool {
	return resolveConsent(viperForce, yesFlag)
}

// ExportDisplayChangesSummary exports displayChangesSummary for testing.
func ExportDisplayChangesSummary(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	displayChangesSummary(cmd, diff)
}

// ExportDiffToJSON exports diffToJSON for testing.
func ExportDiffToJSON(diff *clusterupdate.UpdateResult) DiffJSONOutput {
	return diffToJSON(diff)
}

// ExportConfirmDisruptiveChanges exports confirmDisruptiveChanges for testing.
func ExportConfirmDisruptiveChanges(
	cmd *cobra.Command,
	diff *clusterupdate.UpdateResult,
	force bool,
) (bool, bool) {
	return confirmDisruptiveChanges(cmd, diff, force)
}

// ExportReportFailedChanges exports reportFailedChanges for testing.
func ExportReportFailedChanges(cmd *cobra.Command, result *clusterupdate.UpdateResult) {
	reportFailedChanges(cmd, result)
}

// ErrUpdateChangesFailed exports errUpdateChangesFailed for testing.
var ErrUpdateChangesFailed = errUpdateChangesFailed

// ExportApplyInPlaceChanges exposes applyInPlaceChanges for testing. It builds
// the component reconciler internally so the unexported type is not needed.
func ExportApplyInPlaceChanges(
	cmd *cobra.Command,
	updater clusterprovisioner.Updater,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	ctx *localregistry.Context,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
	force bool,
	allowRolling bool,
) error {
	eksRegion := ""
	if ctx.EKSConfig != nil {
		eksRegion = ctx.EKSConfig.Region
	}

	reconciler := newComponentReconciler(cmd, ctx.ClusterCfg, clusterName, eksRegion)

	return applyInPlaceChanges(
		cmd, updater, reconciler, clusterName,
		currentSpec, ctx, diff, outputTimer, force, allowRolling,
	)
}

// ExportExecuteRecreateFlow exposes the post-consent recreate boundary for fail-closed tests.
func ExportExecuteRecreateFlow(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	clusterName string,
) error {
	orchestrator := &updateOrchestrator{
		cmd:         cmd,
		ctx:         ctx,
		clusterName: clusterName,
		consent:     true,
	}

	return orchestrator.executeRecreateFlow()
}

// ExportFinishRecreateFlow exposes successful recreation finalization for safety tests.
func ExportFinishRecreateFlow(
	ctx *localregistry.Context,
	clusterName string,
	creationErr error,
) error {
	return finishRecreateFlow(context.Background(), ctx, clusterName, creationErr)
}

// ExportClearDeletedEKSState exposes the post-delete state invalidation boundary.
func ExportClearDeletedEKSState(ctx *localregistry.Context, clusterName string) error {
	return clearDeletedEKSState(ctx, clusterName)
}

// ExportComputeUpdateDiff exposes updateOrchestrator.computeUpdateDiff for testing.
func ExportComputeUpdateDiff(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	clusterName string,
	updater clusterprovisioner.Updater,
) (*v1alpha1.ClusterSpec, *clusterupdate.UpdateResult, error) {
	orchestrator := &updateOrchestrator{
		cmd:         cmd,
		ctx:         ctx,
		clusterName: clusterName,
	}

	return orchestrator.computeUpdateDiff(updater)
}

// ExportReportNoApplicableChanges exports reportNoApplicableChanges for testing.
func ExportReportNoApplicableChanges(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	reportNoApplicableChanges(cmd, diff)
}

// ExportOutputFormatJSON exports outputFormatJSON for testing.
const ExportOutputFormatJSON = outputFormatJSON

// ExportOutputFormatText exports outputFormatText for testing.
const ExportOutputFormatText = outputFormatText

// ExportFormatDiffTable exports formatDiffTable for benchmarking.
// The trailing int is vestigial (formatDiffTable now derives counts from diff)
// and is retained so existing benchmark call sites compile unchanged.
func ExportFormatDiffTable(diff *clusterupdate.UpdateResult, _ int) string {
	return formatDiffTable(diff)
}

// ExportStripParenthetical exports stripParenthetical for testing.
func ExportStripParenthetical(s string) string {
	return stripParenthetical(s)
}

// ExportPinnedVersionSkipReason is a test-visible alias for pinnedVersionSkipReason.
type ExportPinnedVersionSkipReason = pinnedVersionSkipReason

// Test-visible aliases for the pinnedVersionSkipReason sentinels.
const (
	ExportPinnedVersionProceed     = pinnedVersionProceed
	ExportPinnedVersionAlreadyAtIt = pinnedVersionAlreadyAtIt
	ExportPinnedVersionNewer       = pinnedVersionNewer
)

// ExportNormalizePinnedVersion exposes normalizePinnedVersion for testing.
func ExportNormalizePinnedVersion(
	rawPinnedVersion, currentVersion string,
) (string, ExportPinnedVersionSkipReason, error) {
	return normalizePinnedVersion(rawPinnedVersion, currentVersion)
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

// ExportNewListResultWithRunState creates a listResult carrying a run-state for testing the STATUS
// column / JSON status field.
func ExportNewListResultWithRunState(
	provider v1alpha1.Provider,
	distribution v1alpha1.Distribution,
	clusterName string,
	runState clusterdiscovery.RunState,
) ExportListResult {
	return listResult{
		Provider:     provider,
		Distribution: distribution,
		ClusterName:  clusterName,
		RunState:     runState,
	}
}

// ExportStatusLabel exports statusLabel for testing the run-state → STATUS vocabulary mapping.
func ExportStatusLabel(runState clusterdiscovery.RunState) string {
	return statusLabel(runState)
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
	return maybeWaitForTTL(cmd, clusterName, clusterCfg, nil)
}

// ExportAutoDeleteCluster exports autoDeleteCluster for offline TTL safety tests.
func ExportAutoDeleteCluster(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	eksConfig *clusterprovisioner.EKSConfig,
) error {
	return autoDeleteCluster(cmd, clusterName, clusterCfg, eksConfig)
}

// ExportDeleteResolvedClusterState exposes exact-target state cleanup for safety tests.
func ExportDeleteResolvedClusterState(resolved *lifecycle.ResolvedClusterInfo) error {
	return deleteResolvedClusterState(resolved)
}

// ExportDeleteTTLClusterState exposes TTL state cleanup for safety tests.
func ExportDeleteTTLClusterState(
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	eksConfig *clusterprovisioner.EKSConfig,
) error {
	return deleteTTLClusterState(clusterName, clusterCfg, eksConfig)
}

// ExportNormalizeVersionTag exposes normalizeVersionTag for testing.
func ExportNormalizeVersionTag(tag string) string {
	return normalizeVersionTag(tag)
}

// ExportVersionsEqual exposes versionsEqual for testing.
func ExportVersionsEqual(current, target string) bool {
	return versionsEqual(current, target)
}

// ExportIsDowngrade exposes isDowngrade for testing.
func ExportIsDowngrade(current, target string) bool {
	return isDowngrade(current, target)
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
	eksRegion ...string,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster", eksRegion...)

	return r.reconcileComponents(context.Background(), diff, result)
}

// ExportReconcileClusterAutoscaler exposes reconcileClusterAutoscaler for unit testing.
func ExportReconcileClusterAutoscaler(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	change clusterupdate.Change,
) error {
	r := newComponentReconciler(cmd, clusterCfg, "test-cluster")

	return r.reconcileClusterAutoscaler(context.Background(), change)
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

// ExportStripDistributionPrefix exports stripDistributionPrefix for testing.
func ExportStripDistributionPrefix(contextName string) string {
	return stripDistributionPrefix(contextName)
}

// ExportDetectClusterDistribution exports detectClusterDistribution for testing.
// It builds a ResolvedClusterInfo for the Docker provider with the given cluster
// name and kubeconfig path, then probes the kubeconfig using the shared
// distribution context prefixes.
func ExportDetectClusterDistribution(
	clusterName, kubeconfigPath string,
) *clusterdetector.Info {
	return detectClusterDistribution(context.Background(), &lifecycle.ResolvedClusterInfo{
		ClusterName:    clusterName,
		Provider:       v1alpha1.ProviderDocker,
		KubeconfigPath: kubeconfigPath,
	})
}

// ExportResolveContextName builds a kubeconfig containing the given context names
// and resolves clusterName against it, exposing resolveContextName for testing.
func ExportResolveContextName(contextNames []string, clusterName string) (string, error) {
	config := clientcmdapi.NewConfig()
	for _, name := range contextNames {
		config.Contexts[name] = clientcmdapi.NewContext()
	}

	return resolveContextName(config, clusterName)
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

// ExportResolveArchivePath exports resolveArchivePath for testing.
func ExportResolveArchivePath(
	args []string,
	deprecatedFlagValue string,
	deprecatedFlagDisplay string,
) (string, error) {
	return resolveArchivePath(args, deprecatedFlagValue, deprecatedFlagDisplay)
}

// ExportCategoryIcon exports categoryIcon for testing.
func ExportCategoryIcon(cat clusterupdate.ChangeCategory) string {
	return categoryIcon(cat)
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

// ExportIsClusterContainer exports IsClusterContainer for testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var ExportIsClusterContainer = IsClusterContainer

// ExportRefreshAndVerifyKubeconfig exports refreshAndVerifyKubeconfig for testing.
func ExportRefreshAndVerifyKubeconfig(
	cmd *cobra.Command,
	refresher clusterprovisioner.KubeconfigRefresher,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	return refreshAndVerifyKubeconfig(cmd, refresher, clusterCfg, clusterName)
}

// ExportSetIsKubeconfigStaleFunc overrides the isKubeconfigStaleFunc for testing.
// The returned function restores the original.
func ExportSetIsKubeconfigStaleFunc(fn func(string, string) bool) func() {
	orig := isKubeconfigStaleFunc
	isKubeconfigStaleFunc = fn

	return func() { isKubeconfigStaleFunc = orig }
}

// ExportSetDeleteUnmanagedGuard overrides the delete command's unmanaged-cluster guard for testing
// (so the refusal path is exercised without a live cross-provider discoverer). The returned function
// restores the original.
func ExportSetDeleteUnmanagedGuard(
	fn func(context.Context, *lifecycle.ResolvedClusterInfo) error,
) func() {
	orig := deleteUnmanagedGuardFunc
	deleteUnmanagedGuardFunc = fn

	return func() { deleteUnmanagedGuardFunc = orig }
}

// ExportRunDiagnoseTextReport exposes runDiagnoseTextReport for testing.
func ExportRunDiagnoseTextReport(report k8s.DiagnoseReport, w io.Writer) error {
	return runDiagnoseTextReport(report, w)
}

// ExportRunDiagnoseJSONReport exposes runDiagnoseJSONReport for testing.
func ExportRunDiagnoseJSONReport(report k8s.DiagnoseReport, w io.Writer) error {
	return runDiagnoseJSONReport(report, w)
}

// ExportSetUpdateUnmanagedGuard overrides the update command's unmanaged-cluster guard for testing
// (so the refusal path is exercised without a live cross-provider discoverer). The returned function
// restores the original.
func ExportSetUpdateUnmanagedGuard(
	fn func(context.Context, *lifecycle.ResolvedClusterInfo) error,
) func() {
	orig := updateUnmanagedGuardFunc
	updateUnmanagedGuardFunc = fn

	return func() { updateUnmanagedGuardFunc = orig }
}

// ExportGuardUpdateTargetManaged exposes guardUpdateTargetManaged for testing — it resolves the
// kubeconfig from the ClusterCfg and applies the (overridable) unmanaged-cluster guard.
func ExportGuardUpdateTargetManaged(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	eksConfig *clusterprovisioner.EKSConfig,
) error {
	_, _, err := guardUpdateTargetManaged(ctx, clusterCfg, clusterName, eksConfig)

	return err
}
