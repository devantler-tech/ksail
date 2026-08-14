package workload

import (
	"context"
	"io"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/devantler-tech/ksail/v7/pkg/svc/hostdebug"
	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/workloadwatch"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Test exports for unexported workload helpers used by external-package tests.
// These seams cover validation/expansion helpers, debounce/watch behavior, and
// source/Flux path resolution and formatting. They are only compiled during
// testing and should be changed together with the tests that depend on them.

// The Flux-substitution engine now lives in pkg/svc/fluxsubst; these shims
// delegate to its exported API so existing command-package tests keep working.
var (
	ExportExpandFluxSubstitutions = fluxsubst.ExpandFluxSubstitutions //nolint:gochecknoglobals // test export
	ExportGetSchemaTypeAtPath     = fluxsubst.GetSchemaTypeAtPath     //nolint:gochecknoglobals // test export
	ExportSchemaURLs              = fluxsubst.SchemaURLs              //nolint:gochecknoglobals // test export
	ExportSplitAPIVersion         = fluxsubst.SplitAPIVersion         //nolint:gochecknoglobals // test export
	ExportTypedPlaceholderValue   = fluxsubst.TypedPlaceholderValue   //nolint:gochecknoglobals // test export
)

// ExportAttributionFromDocuments exposes attributionFromDocuments so external-package
// tests can assert the render-provenance→failure-attribution mapping (layer tagging,
// stream-origin skipping, and ambiguous-identity dropping) without a full render run.
var ExportAttributionFromDocuments = attributionFromDocuments //nolint:gochecknoglobals // test export

// ExportDocumentIdentityFromObject exposes documentIdentityFromObject so external-package
// tests can assert the Kind/Namespace/Name identity derivation used to attribute CEL
// rule violations.
var ExportDocumentIdentityFromObject = documentIdentityFromObject //nolint:gochecknoglobals // test export

// ExportDecodeDocumentObject exposes decodeDocumentObject so external-package tests can
// assert the empty/non-mapping document skipping used before CEL evaluation.
var ExportDecodeDocumentObject = decodeDocumentObject //nolint:gochecknoglobals // test export

// ExportDebounceState is an exported type alias for the workloadwatch debounce
// state. The debounce/poll machinery moved to pkg/svc/workloadwatch; the shims
// below delegate to its exported API so existing command-package tests keep
// working.
type ExportDebounceState = workloadwatch.DebounceState

// ExportDebounceInterval exposes the debounce interval constant for testing.
const ExportDebounceInterval = workloadwatch.DebounceInterval

// ExportIsRelevantEvent exposes the engine's IsRelevantEvent for testing.
func ExportIsRelevantEvent(event fsnotify.Event) bool {
	return workloadwatch.IsRelevantEvent(event)
}

// ExportResolveSourceDir exposes resolveSourceDir for testing.
func ExportResolveSourceDir(cfg *v1alpha1.Cluster, pathFlag string) string {
	return resolveSourceDir(cfg, pathFlag)
}

// ExportAddRecursive exposes the engine's AddRecursive for testing.
func ExportAddRecursive(watcher *fsnotify.Watcher, root string) error {
	return workloadwatch.AddRecursive(watcher, root) //nolint:wrapcheck // test shim, verbatim error
}

// ExportNewDebounceState creates a new zero-value debounce state for testing.
func ExportNewDebounceState() *ExportDebounceState {
	return &ExportDebounceState{}
}

// ExportSetDebounceState sets the generation and lastFile fields under a mutex.
func ExportSetDebounceState(state *ExportDebounceState, generation uint64, lastFile string) {
	state.Set(generation, lastFile)
}

// ExportGetGeneration returns the current generation counter.
func ExportGetGeneration(state *ExportDebounceState) uint64 {
	return state.Generation()
}

// ExportGetLastFile returns the current lastFile value.
func ExportGetLastFile(state *ExportDebounceState) string {
	return state.LastFile()
}

// ExportCancelPendingDebounce exposes the engine's CancelPendingDebounce for testing.
func ExportCancelPendingDebounce(state *ExportDebounceState) {
	workloadwatch.CancelPendingDebounce(state)
}

// ExportScheduleApply exposes the engine's ScheduleApply for testing.
func ExportScheduleApply(state *ExportDebounceState, file string, applyCh chan string) {
	workloadwatch.ScheduleApply(state, file, applyCh)
}

// ExportEnqueueIfCurrent exposes the engine's EnqueueIfCurrent for testing.
func ExportEnqueueIfCurrent(state *ExportDebounceState, expectedGen uint64, applyCh chan string) {
	workloadwatch.EnqueueIfCurrent(state, expectedGen, applyCh)
}

// ExportTryAddDirectory exposes the engine's TryAddDirectory for testing.
func ExportTryAddDirectory(watcher *fsnotify.Watcher, path string, cmd *cobra.Command) {
	workloadwatch.TryAddDirectory(watcher, path, cmd.ErrOrStderr())
}

// ExportFindKustomizationDir exposes the engine's FindKustomizationDir for testing.
func ExportFindKustomizationDir(changedFile, rootDir string) string {
	return workloadwatch.FindKustomizationDir(changedFile, rootDir, hasKustomizationFile)
}

// ExportMatchFluxKustomizations exposes the engine's MatchFluxKustomizations for testing.
func ExportMatchFluxKustomizations(
	changedDir, rootDir string,
	kustomizations []flux.KustomizationInfo,
) []string {
	return workloadwatch.MatchFluxKustomizations(changedDir, rootDir, kustomizations)
}

// ExportNormalizeFluxPath exposes the engine's NormalizeFluxPath for testing.
func ExportNormalizeFluxPath(p string) string {
	return workloadwatch.NormalizeFluxPath(p)
}

// ExportFormatElapsed exposes the engine's FormatElapsed for testing.
func ExportFormatElapsed(d time.Duration) string {
	return workloadwatch.FormatElapsed(d)
}

// ExportHasKustomizationFile exposes hasKustomizationFile for testing.
func ExportHasKustomizationFile(dir string) bool {
	return hasKustomizationFile(dir)
}

// ExportResolveScanInput exposes resolveScanInput for testing the scan render
// decision (rendered temp dir vs raw path) and temp-dir cleanup without
// invoking kubescape.
func ExportResolveScanInput(
	ctx context.Context,
	cmd *cobra.Command,
	path string,
	cfg *v1alpha1.Cluster,
	configFound, noRender bool,
) (string, func(), error) {
	return resolveScanInput(ctx, cmd, path, cfg, configFound, noRender)
}

// ExportResolveScanOutput exposes resolveScanOutput for testing the scan
// --output directory creation and canonicalization.
func ExportResolveScanOutput(output string) (string, error) {
	return resolveScanOutput(output)
}

// ExportCanonicalizeSchemaLocations exposes canonicalizeSchemaLocations for
// testing local-path canonicalization vs URL/template passthrough.
func ExportCanonicalizeSchemaLocations(locations []string) []string {
	return canonicalizeSchemaLocations(locations)
}

// ExportResolveCELRulesPath exposes resolveCELRulesPath so external tests can
// assert the --rules flag > spec.workload.validation.rules > "" precedence
// without running a full validate.
func ExportResolveCELRulesPath(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
	rulesFlag string,
) string {
	return resolveCELRulesPath(cmd, cfg, configFound, loadErr, rulesFlag)
}

// ExportScanSettings mirrors the resolved scan settings for external tests.
type ExportScanSettings struct {
	Frameworks          []string
	ComplianceThreshold float32
	Exceptions          string
}

// ExportResolveScanSettings reads the scan flags off cmd (which must be a
// NewScanCmd() whose flags have been parsed) and merges them with
// spec.workload.scan, returning the resolved settings. It lets external tests
// exercise the flag > config > default precedence without invoking kubescape.
func ExportResolveScanSettings(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	configFound bool,
) (ExportScanSettings, error) {
	frameworks, _ := cmd.Flags().GetStringSlice("framework")
	threshold, _ := cmd.Flags().GetFloat32("compliance-threshold")
	exceptions, _ := cmd.Flags().GetString("exceptions")

	settings, err := resolveScanSettings(cmd, scanFlags{
		frameworks:          frameworks,
		complianceThreshold: threshold,
		exceptions:          exceptions,
	}, cfg, configFound)

	return ExportScanSettings{
		Frameworks:          settings.frameworks,
		ComplianceThreshold: settings.complianceThreshold,
		Exceptions:          settings.exceptions,
	}, err
}

// ExportPollInterval exposes the engine's poll interval constant for testing.
const ExportPollInterval = workloadwatch.PollInterval

// ExportFileSnapshot is an exported type alias for the workloadwatch file snapshot.
type ExportFileSnapshot = workloadwatch.FileSnapshot

// ExportBuildFileSnapshot exposes the engine's BuildFileSnapshot for testing.
func ExportBuildFileSnapshot(dir string) ExportFileSnapshot {
	return workloadwatch.BuildFileSnapshot(dir)
}

// ExportDetectChangedFile exposes the engine's DetectChangedFile for testing.
func ExportDetectChangedFile(dir string, snapshot ExportFileSnapshot) string {
	return workloadwatch.DetectChangedFile(dir, snapshot)
}

// ExportTopologicalSortKustomizations exposes topologicalSortKustomizations for testing.
func ExportTopologicalSortKustomizations(
	kustomizations []flux.KustomizationInfo,
) []flux.KustomizationInfo {
	return topologicalSortKustomizations(kustomizations)
}

// ExportParseInteger exposes the engine's parseInteger for testing.
func ExportParseInteger(trimmed, defaultVal string) any {
	return fluxsubst.ParseInteger(trimmed, defaultVal)
}

// ExportParseNumber exposes the engine's parseNumber for testing.
func ExportParseNumber(trimmed, defaultVal string) any {
	return fluxsubst.ParseNumber(trimmed, defaultVal)
}

// ExportParseBoolean exposes the engine's parseBoolean for testing.
func ExportParseBoolean(trimmed, defaultVal string) any {
	return fluxsubst.ParseBoolean(trimmed, defaultVal)
}

// ExportInferYAMLType exposes the engine's inferYAMLType for testing.
func ExportInferYAMLType(trimmed, defaultVal string) any {
	return fluxsubst.InferYAMLType(trimmed, defaultVal)
}

// ExportSchemaNodeType exposes the engine's schemaNodeType for testing.
func ExportSchemaNodeType(schema map[string]any) string {
	return fluxsubst.SchemaNodeType(schema)
}

// ExportIsNumericIndex exposes the engine's isNumericIndex for testing.
func ExportIsNumericIndex(str string) bool {
	return fluxsubst.IsNumericIndex(str)
}

// ExportParseJSONSchema exposes the engine's parseJSONSchema for testing.
func ExportParseJSONSchema(data []byte) map[string]any {
	return fluxsubst.ParseJSONSchema(data)
}

// ExportResolveFromProperties exposes the engine's resolveFromProperties for testing.
func ExportResolveFromProperties(schema map[string]any, key string) map[string]any {
	return fluxsubst.ResolveFromProperties(schema, key)
}

// ExportResolveFromItems exposes the engine's resolveFromItems for testing.
func ExportResolveFromItems(schema map[string]any, key string) map[string]any {
	return fluxsubst.ResolveFromItems(schema, key)
}

// ExportResolveFromCombiners exposes the engine's resolveFromCombiners for testing.
func ExportResolveFromCombiners(schema map[string]any, key string) map[string]any {
	return fluxsubst.ResolveFromCombiners(schema, key)
}

// ExportSchemaCacheDir exposes the engine's schemaCacheDir for testing.
func ExportSchemaCacheDir() string {
	return fluxsubst.SchemaCacheDir()
}

// ExportSchemaCacheFileName exposes the engine's schemaCacheFileName for testing.
func ExportSchemaCacheFileName(schemaURL string) string {
	return fluxsubst.SchemaCacheFileName(schemaURL)
}

// ExportDistributionToLabelScheme exposes the hostdebug engine's
// DistributionToLabelScheme for testing (the mapping moved to pkg/svc/hostdebug).
func ExportDistributionToLabelScheme(
	distribution v1alpha1.Distribution,
) dockerprovider.LabelScheme {
	return hostdebug.DistributionToLabelScheme(distribution)
}

// ExportOutputPlain exposes outputPlain for testing.
func ExportOutputPlain(cmd *cobra.Command, images []string) error {
	return outputPlain(cmd, images)
}

// ExportOutputJSON exposes outputJSON for testing.
func ExportOutputJSON(cmd *cobra.Command, images []string) error {
	return outputJSON(cmd, images)
}

// ExportFailedKustomizations is an exported type alias for the unexported
// failedKustomizations struct. It lets test code hold and manipulate the
// shared failure tracker without accessing unexported fields directly.
type ExportFailedKustomizations = failedKustomizations

// ExportRecordKustomizationFailure records a permanent failure for name in the tracker.
func ExportRecordKustomizationFailure(f *ExportFailedKustomizations, name string, err error) {
	f.record(name, err)
}

// ExportCheckKustomizationDependencies returns an error if any listed
// dependency has permanently failed in the tracker.
func ExportCheckKustomizationDependencies(f *ExportFailedKustomizations, dependsOn []string) error {
	return f.checkDependencies(dependsOn)
}

// ExportErrDependencyBlocked exposes the cascade sentinel for testing.
func ExportErrDependencyBlocked() error { return errDependencyBlocked }

// ExportErrKustomizationReconcile exposes the kustomization-reconcile sentinel for testing.
func ExportErrKustomizationReconcile() error { return errKustomizationReconcile }

// ExportIsAggregatedReconcileError exposes isAggregatedReconcileError for testing.
func ExportIsAggregatedReconcileError(err error) bool {
	return isAggregatedReconcileError(err)
}

// ExportPollUntilKustomizationReady exposes pollUntilKustomizationReady for testing.
func ExportPollUntilKustomizationReady(
	ctx context.Context,
	fluxReconciler *flux.Reconciler,
	name string,
	dependsOn []string,
	failed *ExportFailedKustomizations,
) error {
	return pollUntilKustomizationReady(ctx, fluxReconciler, name, dependsOn, failed)
}

// ExportIsKustomizationExcluded exposes isKustomizationExcluded for testing.
func ExportIsKustomizationExcluded(
	kustomization flux.KustomizationInfo,
	excludeSet map[string]bool,
) bool {
	return isKustomizationExcluded(kustomization, excludeSet)
}

// ExportRetryOnTransientError exposes retryOnTransientError for unit testing.
func ExportRetryOnTransientError(
	ctx context.Context,
	cmd *cobra.Command,
	maxAttempts int,
	baseWait, maxWait time.Duration,
	operation func() error,
) error {
	return retryOnTransientError(ctx, cmd, maxAttempts, baseWait, maxWait, operation)
}

// ExportRunWatch exposes runWatch for unit testing the early-exit path
// validation that runs before config load.
func ExportRunWatch(cmd *cobra.Command, pathFlag string, initialApply bool, debug bool) error {
	return runWatch(cmd, pathFlag, initialApply, debug, nil)
}

// ExportRunHooks exposes runHooks for unit testing hook execution.
func ExportRunHooks(ctx context.Context, cmd *cobra.Command, hooks []string) error {
	return runHooks(ctx, cmd, hooks)
}

// ErrHookFailed exposes the errHookFailed sentinel for test assertions.
var ErrHookFailed = errHookFailed

// ErrCNINotCiliumExport exposes the network command's CNI-guard sentinel.
var ErrCNINotCiliumExport = ErrCNINotCilium

// ExportSetFlowObserverFactory swaps the network command's observer factory so
// tests can inject a fake observer without a live Hubble relay. It returns a
// restore function that reinstates the original factory.
func ExportSetFlowObserverFactory(factory func(string) hubble.FlowObserver) func() {
	original := newFlowObserver
	newFlowObserver = factory

	return func() { newFlowObserver = original }
}

// ExportSetMirrorClients swaps the mirror command's client factory so tests
// can inject a fake clientset and REST config without a live cluster. It
// returns a restore function that reinstates the original factory.
func ExportSetMirrorClients(
	factory func(string, string) (kubernetes.Interface, *rest.Config, error),
) func() {
	original := newMirrorClients
	newMirrorClients = factory

	return func() { newMirrorClients = original }
}

// ExportSetRunCaptureSession swaps the mirror command's blocking capture call
// so tests can substitute the exec-channel stream. It returns a restore
// function that reinstates the original.
func ExportSetRunCaptureSession(
	session func(
		ctx context.Context,
		client kubernetes.Interface,
		config *rest.Config,
		point *mirror.TapPoint,
		port int,
		out io.Writer,
		opts ...mirror.CaptureSessionOption,
	) error,
) func() {
	original := runCaptureSession
	runCaptureSession = session

	return func() { runCaptureSession = original }
}

// ExportSetNewLiveReplay swaps how the mirror command builds its --to live
// replay sink so tests can inject a capturing dialer. It returns a restore
// function that reinstates the original.
func ExportSetNewLiveReplay(
	factory func(address string, port int) (*mirror.LiveReplay, error),
) func() {
	original := newLiveReplay
	newLiveReplay = factory

	return func() { newLiveReplay = original }
}

// ExportSetRunInterceptSession swaps the intercept command's blocking
// steering-tunnel call so tests can substitute the tunnel without a live
// cluster. It returns a restore function that reinstates the original.
func ExportSetRunInterceptSession(
	session func(
		ctx context.Context,
		client kubernetes.Interface,
		config *rest.Config,
		point *mirror.TapPoint,
		steerCommand []string,
		localPort int,
		keepalive bool,
	) error,
) func() {
	original := runInterceptSession
	runInterceptSession = session

	return func() { runInterceptSession = original }
}

// EphemeralCluster re-exports the internal --ephemeral connection handle so
// external-package tests can assert on what runFn receives.
type EphemeralCluster = ephemeralCluster

// ExportEphemeralBackend is the test-visible form of the backend lifecycle
// bundle. Production keeps the fields private so only withEphemeralCluster can
// order cluster deletion before local workspace cleanup.
type ExportEphemeralBackend struct {
	Provisioner clusterprovisioner.Provisioner
	Cluster     EphemeralCluster
	Cleanup     func() error
}

// ExportSetEphemeralBackendFactory swaps the complete --ephemeral backend so
// tests can control provisioner, connection, and local-cleanup behavior without
// Docker. It returns a restore function that reinstates the original.
func ExportSetEphemeralBackendFactory(
	factory func(name string) (ExportEphemeralBackend, error),
) func() {
	original := newEphemeralBackend
	newEphemeralBackend = func(name string) (ephemeralBackend, error) {
		backend, err := factory(name)

		return ephemeralBackend{
			provisioner: backend.Provisioner,
			cluster:     backend.Cluster,
			cleanup:     backend.Cleanup,
		}, err
	}

	return func() { newEphemeralBackend = original }
}

// ExportCreateEphemeralBackend exposes the concrete production factory for a
// unit test that verifies Kind selection and isolated kubeconfig ownership.
// It only constructs dependencies; it does not provision a cluster.
func ExportCreateEphemeralBackend(name string) (ExportEphemeralBackend, error) {
	backend, err := createEphemeralBackend(name)
	if err != nil {
		return ExportEphemeralBackend{}, err
	}

	return ExportEphemeralBackend{
		Provisioner: backend.provisioner,
		Cluster:     backend.cluster,
		Cleanup:     backend.cleanup,
	}, nil
}

// ExportSetEphemeralClusterWaiter swaps the --ephemeral cluster readiness
// waiter so tests can substitute a fake without a live cluster to poll. It
// returns a restore function that reinstates the original.
func ExportSetEphemeralClusterWaiter(
	wait func(ctx context.Context, kubeconfigPath, contextName string) error,
) func() {
	original := waitForEphemeralCluster
	waitForEphemeralCluster = wait

	return func() { waitForEphemeralCluster = original }
}

// ExportWithEphemeralCluster exposes withEphemeralCluster for external-package
// tests exercising the provision/readiness/guaranteed-teardown seam directly.
func ExportWithEphemeralCluster(
	ctx context.Context,
	cmd *cobra.Command,
	runFn func(ctx context.Context, cluster EphemeralCluster) error,
) error {
	return withEphemeralCluster(ctx, cmd, runFn)
}

// ExportSetEphemeralHelmClient swaps the --ephemeral install-capable Helm
// client factory for tests and returns a restore func.
func ExportSetEphemeralHelmClient(
	factory func(kubeconfigPath, kubeContext string) (helm.Interface, error),
) func() {
	original := newEphemeralHelmClient
	newEphemeralHelmClient = factory

	return func() { newEphemeralHelmClient = original }
}

// ExportInstallDeclaredCharts exposes installDeclaredCharts for
// external-package tests exercising the Phase 3b-2 install step directly.
func ExportInstallDeclaredCharts(
	ctx context.Context,
	cmd *cobra.Command,
	cluster EphemeralCluster,
	sourcePath string,
) error {
	return installDeclaredCharts(ctx, cmd, cluster, sourcePath)
}
