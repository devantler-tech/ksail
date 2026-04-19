package workload

import (
	"context"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/client/flux"
	dockerprovider "github.com/devantler-tech/ksail/v6/pkg/svc/provider/docker"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

// Test exports for unexported workload helpers used by external-package tests.
// These seams cover validation/expansion helpers, debounce/watch behavior, and
// source/Flux path resolution and formatting. They are only compiled during
// testing and should be changed together with the tests that depend on them.

var (
	ExportExpandFluxSubstitutions = expandFluxSubstitutions //nolint:gochecknoglobals // test export
	ExportGetSchemaTypeAtPath     = getSchemaTypeAtPath     //nolint:gochecknoglobals // test export
	ExportSchemaURLs              = schemaURLs              //nolint:gochecknoglobals // test export
	ExportSplitAPIVersion         = splitAPIVersion         //nolint:gochecknoglobals // test export
	ExportTypedPlaceholderValue   = typedPlaceholderValue   //nolint:gochecknoglobals // test export
)

// ExportDebounceState is an exported type alias for the unexported debounceState
// struct. It lets test code in package workload_test hold an opaque handle and
// pass it to the exported test helpers below without accessing unexported fields
// directly.
type ExportDebounceState = debounceState

// ExportDebounceInterval exposes the debounceInterval constant for testing.
const ExportDebounceInterval = debounceInterval

// ExportIsRelevantEvent exposes isRelevantEvent for testing.
func ExportIsRelevantEvent(event fsnotify.Event) bool {
	return isRelevantEvent(event)
}

// ExportResolveSourceDir exposes resolveSourceDir for testing.
func ExportResolveSourceDir(cfg *v1alpha1.Cluster, pathFlag string) string {
	return resolveSourceDir(cfg, pathFlag)
}

// ExportAddRecursive exposes addRecursive for testing.
func ExportAddRecursive(watcher *fsnotify.Watcher, root string) error {
	return addRecursive(watcher, root)
}

// ExportNewDebounceState creates a new zero-value debounceState for testing.
func ExportNewDebounceState() *ExportDebounceState {
	return &ExportDebounceState{}
}

// ExportSetDebounceState sets the generation and lastFile fields under a mutex.
func ExportSetDebounceState(state *ExportDebounceState, generation uint64, lastFile string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.generation = generation
	state.lastFile = lastFile
}

// ExportGetGeneration returns the current generation counter.
func ExportGetGeneration(state *ExportDebounceState) uint64 {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	return state.generation
}

// ExportGetLastFile returns the current lastFile value.
func ExportGetLastFile(state *ExportDebounceState) string {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	return state.lastFile
}

// ExportCancelPendingDebounce exposes cancelPendingDebounce for testing.
func ExportCancelPendingDebounce(state *ExportDebounceState) {
	cancelPendingDebounce(state)
}

// ExportScheduleApply exposes scheduleApply for testing.
func ExportScheduleApply(state *ExportDebounceState, file string, applyCh chan string) {
	scheduleApply(state, file, applyCh)
}

// ExportEnqueueIfCurrent exposes enqueueIfCurrent for testing.
func ExportEnqueueIfCurrent(state *ExportDebounceState, expectedGen uint64, applyCh chan string) {
	enqueueIfCurrent(state, expectedGen, applyCh)
}

// ExportTryAddDirectory exposes tryAddDirectory for testing.
func ExportTryAddDirectory(watcher *fsnotify.Watcher, path string, cmd *cobra.Command) {
	tryAddDirectory(watcher, path, cmd)
}

// ExportFindKustomizationDir exposes findKustomizationDir for testing.
func ExportFindKustomizationDir(changedFile, rootDir string) string {
	return findKustomizationDir(changedFile, rootDir)
}

// ExportMatchFluxKustomizations exposes matchFluxKustomizations for testing.
func ExportMatchFluxKustomizations(
	changedDir, rootDir string,
	kustomizations []flux.KustomizationInfo,
) []string {
	return matchFluxKustomizations(changedDir, rootDir, kustomizations)
}

// ExportNormalizeFluxPath exposes normalizeFluxPath for testing.
func ExportNormalizeFluxPath(p string) string {
	return normalizeFluxPath(p)
}

// ExportFormatElapsed exposes formatElapsed for testing.
func ExportFormatElapsed(d time.Duration) string {
	return formatElapsed(d)
}

// ExportHasKustomizationFile exposes hasKustomizationFile for testing.
func ExportHasKustomizationFile(dir string) bool {
	return hasKustomizationFile(dir)
}

// ExportPollInterval exposes the pollInterval constant for testing.
const ExportPollInterval = pollInterval

// ExportFileSnapshot is an exported type alias for the unexported fileSnapshot.
type ExportFileSnapshot = fileSnapshot

// ExportBuildFileSnapshot exposes buildFileSnapshot for testing.
func ExportBuildFileSnapshot(dir string) ExportFileSnapshot {
	return buildFileSnapshot(dir)
}

// ExportDetectChangedFile exposes detectChangedFile for testing.
func ExportDetectChangedFile(dir string, snapshot ExportFileSnapshot) string {
	return detectChangedFile(dir, snapshot)
}

// ExportTopologicalSortKustomizations exposes topologicalSortKustomizations for testing.
func ExportTopologicalSortKustomizations(
	kustomizations []flux.KustomizationInfo,
) []flux.KustomizationInfo {
	return topologicalSortKustomizations(kustomizations)
}

// ExportParseInteger exposes parseInteger for testing.
func ExportParseInteger(trimmed, defaultVal string) any {
	return parseInteger(trimmed, defaultVal)
}

// ExportParseNumber exposes parseNumber for testing.
func ExportParseNumber(trimmed, defaultVal string) any {
	return parseNumber(trimmed, defaultVal)
}

// ExportParseBoolean exposes parseBoolean for testing.
func ExportParseBoolean(trimmed, defaultVal string) any {
	return parseBoolean(trimmed, defaultVal)
}

// ExportInferYAMLType exposes inferYAMLType for testing.
func ExportInferYAMLType(trimmed, defaultVal string) any {
	return inferYAMLType(trimmed, defaultVal)
}

// ExportSchemaNodeType exposes schemaNodeType for testing.
func ExportSchemaNodeType(schema map[string]any) string {
	return schemaNodeType(schema)
}

// ExportIsNumericIndex exposes isNumericIndex for testing.
func ExportIsNumericIndex(str string) bool {
	return isNumericIndex(str)
}

// ExportParseJSONSchema exposes parseJSONSchema for testing.
func ExportParseJSONSchema(data []byte) map[string]any {
	return parseJSONSchema(data)
}

// ExportResolveFromProperties exposes resolveFromProperties for testing.
func ExportResolveFromProperties(schema map[string]any, key string) map[string]any {
	return resolveFromProperties(schema, key)
}

// ExportResolveFromItems exposes resolveFromItems for testing.
func ExportResolveFromItems(schema map[string]any, key string) map[string]any {
	return resolveFromItems(schema, key)
}

// ExportResolveFromCombiners exposes resolveFromCombiners for testing.
func ExportResolveFromCombiners(schema map[string]any, key string) map[string]any {
	return resolveFromCombiners(schema, key)
}

// ExportSchemaCacheDir exposes schemaCacheDir for testing.
func ExportSchemaCacheDir() string {
	return schemaCacheDir()
}

// ExportSchemaCacheFileName exposes schemaCacheFileName for testing.
func ExportSchemaCacheFileName(schemaURL string) string {
	return schemaCacheFileName(schemaURL)
}

// ExportDistributionToLabelScheme exposes distributionToLabelScheme for testing.
func ExportDistributionToLabelScheme(
	distribution v1alpha1.Distribution,
) dockerprovider.LabelScheme {
	return distributionToLabelScheme(distribution)
}

// ExportOutputPlain exposes outputPlain for testing.
func ExportOutputPlain(cmd *cobra.Command, images []string) error {
	return outputPlain(cmd, images, nil)
}

// ExportOutputJSON exposes outputJSON for testing.
func ExportOutputJSON(cmd *cobra.Command, images []string) error {
	return outputJSON(cmd, images, nil)
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
