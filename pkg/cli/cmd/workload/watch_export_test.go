package workload

import (
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
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
func ExportNewDebounceState() *debounceState {
	return &debounceState{}
}

// ExportSetDebounceState atomically sets the generation and lastFile fields.
func ExportSetDebounceState(state *debounceState, generation uint64, lastFile string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.generation = generation
	state.lastFile = lastFile
}

// ExportGetGeneration returns the current generation counter.
func ExportGetGeneration(state *debounceState) uint64 {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	return state.generation
}

// ExportGetLastFile returns the current lastFile value.
func ExportGetLastFile(state *debounceState) string {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	return state.lastFile
}

// ExportCancelPendingDebounce exposes cancelPendingDebounce for testing.
func ExportCancelPendingDebounce(state *debounceState) {
	cancelPendingDebounce(state)
}

// ExportScheduleApply exposes scheduleApply for testing.
func ExportScheduleApply(state *debounceState, file string, applyCh chan string) {
	scheduleApply(state, file, applyCh)
}

// ExportEnqueueIfCurrent exposes enqueueIfCurrent for testing.
func ExportEnqueueIfCurrent(state *debounceState, expectedGen uint64, applyCh chan string) {
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

// ExportFormatElapsed exposes formatElapsed for testing.
func ExportFormatElapsed(d time.Duration) string {
	return formatElapsed(d)
}
