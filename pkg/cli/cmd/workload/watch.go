package workload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/flux"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// debounceInterval is the time to wait after the last file event before
// triggering an apply. This prevents redundant reconciles during batch saves.
const debounceInterval = 500 * time.Millisecond

var errNotDirectory = errors.New("watch path is not a directory")

// NewWatchCmd creates the workload watch command.
func NewWatchCmd() *cobra.Command {
	var (
		pathFlag     string
		initialApply bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch for file changes and auto-apply workloads",
		Long: `Watch a directory for file changes and automatically apply workloads.

When files in the watched directory are created, modified, or deleted,
the command debounces changes (~500ms) then scopes the apply to the
nearest directory containing a kustomization.yaml file, walking up from
the changed file to the watch root. If no kustomization.yaml boundary is
found, or the boundary is the watch root, it applies the full root
directory. This scoping ensures only the affected Kustomize layer is
re-applied, making iteration faster in monorepo-style layouts.

Each reconcile prints a timestamped status line showing the changed file,
the outcome (success or failure), and the elapsed time for the apply.
Press Ctrl+C to stop the watcher.

Use --initial-apply to synchronize the cluster with the current state of
the watch directory before entering the watch loop. This is useful after
editing manifests offline or when starting a fresh session.

Examples:
  # Watch the default k8s/ directory
  ksail workload watch

  # Watch and apply once on startup before entering the loop
  ksail workload watch --initial-apply

  # Watch a custom directory
  ksail workload watch --path=./manifests`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().StringVar(
		&pathFlag, "path", "",
		"Directory to watch for changes (default: k8s/ or spec.workload.sourceDirectory from ksail.yaml)",
	)

	cmd.Flags().BoolVar(
		&initialApply, "initial-apply", false,
		"Apply all workloads once immediately on startup before entering the watch loop",
	)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runWatch(cmd, pathFlag, initialApply)
	}

	return cmd
}

// runWatch starts the file watcher loop.
func runWatch(cmd *cobra.Command, pathFlag string, initialApply bool) error {
	cmdCtx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	watchDir := resolveSourceDir(cmdCtx.ClusterCfg, pathFlag)

	// Verify the directory exists.
	info, err := os.Stat(watchDir)
	if err != nil {
		return fmt.Errorf("access watch directory %q: %w", watchDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%q: %w", watchDir, errNotDirectory)
	}

	// Canonicalize the watch directory (resolve symlinks + absolute) so that
	// file events are matched against the real directory and symlink-escape
	// attacks are prevented in CI pipelines processing external manifests.
	absDir, err := fsutil.EvalCanonicalPath(watchDir)
	if err != nil {
		return fmt.Errorf("resolve watch directory %q: %w", watchDir, err)
	}

	// Try to create a Flux reconciler for selective Kustomization reconciliation.
	// If Flux is not available (CRDs not installed, kubeconfig error, etc.),
	// the reconciler is nil and selective reconciliation is silently skipped.
	fluxReconciler := tryCreateFluxReconciler()

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "👁️",
		Content: "Watching for changes...",
		Writer:  cmd.OutOrStdout(),
	})

	cmd.PrintErrf("  watching: %s\n", absDir)
	cmd.PrintErrf("  press Ctrl+C to stop\n\n")

	return watchLoop(cmd.Context(), cmd, absDir, initialApply, fluxReconciler)
}

// watchLoop sets up the fsnotify watcher and runs the debounced apply loop.
// When initialApply is true, a full apply of the watch root is performed
// after the event loop goroutine is started, so watcher events are consumed
// immediately and not dropped or buffered during the initial apply. Ctrl+C
// cancels both the initial apply and the event loop via the shared sigCtx.
func watchLoop(
	ctx context.Context,
	cmd *cobra.Command,
	dir string,
	initialApply bool,
	fluxReconciler *flux.Reconciler,
) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create file watcher: %w", err)
	}

	defer func() { _ = watcher.Close() }()

	// Add all directories recursively.
	err = addRecursive(watcher, dir)
	if err != nil {
		return err
	}

	// Set up signal handling for graceful shutdown.
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the event loop before any apply so that watcher events are
	// consumed immediately, avoiding backlogs or drops during the initial apply.
	errCh := make(chan error, 1)

	go func() {
		errCh <- eventLoop(sigCtx, cmd, watcher, dir, fluxReconciler)
	}()

	if initialApply {
		executeAndReportApply(sigCtx, cmd, dir, "initial apply")
	}

	// Wait for the event loop to complete and propagate its error.
	return <-errCh
}

// debounceState holds the mutable state shared between the event loop and
// debounce timer callbacks.
type debounceState struct {
	timer      *time.Timer
	mutex      sync.Mutex
	lastFile   string
	generation uint64
}

// eventLoop processes fsnotify events with debouncing.
//
// Applies are serialized through a single worker goroutine fed by a
// capacity-1 channel (applyCh). Rapid file events are coalesced: if a
// pending apply is already queued the stale entry is replaced with the
// latest changed file before the worker consumes it.
func eventLoop(
	ctx context.Context,
	cmd *cobra.Command,
	watcher *fsnotify.Watcher,
	dir string,
	fluxReconciler *flux.Reconciler,
) error {
	state := &debounceState{}

	// applyCh serializes applies.  Capacity 1 ensures at most one apply is
	// pending at any time; coalescing replaces a queued entry with the latest.
	applyCh := make(chan string, 1)

	// Single worker: runs applies one at a time, stops when ctx is cancelled.
	go applyWorker(ctx, cmd, dir, applyCh, fluxReconciler)

	defer cancelPendingDebounce(state)

	return dispatchEvents(ctx, cmd, watcher, state, applyCh)
}

// applyWorker runs applies one at a time, stopping when ctx is cancelled.
// The kustomization cache is owned exclusively by this single goroutine,
// so no mutex is needed.
func applyWorker(
	ctx context.Context,
	cmd *cobra.Command,
	dir string,
	applyCh <-chan string,
	fluxReconciler *flux.Reconciler,
) {
	var cachedKustomizations []flux.KustomizationInfo

	for {
		select {
		case <-ctx.Done():
			return
		case file, ok := <-applyCh:
			if !ok {
				return
			}

			applyAndReport(ctx, cmd, dir, file, fluxReconciler, &cachedKustomizations)
		}
	}
}

// cancelPendingDebounce increments the generation counter to invalidate any
// pending timer callback and stops the timer if active.
func cancelPendingDebounce(state *debounceState) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.generation++

	if state.timer != nil {
		state.timer.Stop()
	}
}

// dispatchEvents reads fsnotify events and errors, debouncing file changes
// before enqueuing them on applyCh.
func dispatchEvents(
	ctx context.Context,
	cmd *cobra.Command,
	watcher *fsnotify.Watcher,
	state *debounceState,
	applyCh chan string,
) error {
	for {
		select {
		case <-ctx.Done():
			cmd.PrintErrln("\n✋ watcher stopped")

			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			handleFileEvent(event, watcher, cmd, state, applyCh)

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}

			return fmt.Errorf("file watcher: %w", watchErr)
		}
	}
}

// handleFileEvent processes a single fsnotify event: filters irrelevant ops,
// registers new directories, and schedules a debounced apply.
func handleFileEvent(
	event fsnotify.Event,
	watcher *fsnotify.Watcher,
	cmd *cobra.Command,
	state *debounceState,
	applyCh chan string,
) {
	if !isRelevantEvent(event) {
		return
	}

	// If a new directory was created, watch it too.
	if event.Has(fsnotify.Create) {
		tryAddDirectory(watcher, event.Name, cmd)
	}

	scheduleApply(state, event.Name, applyCh)
}

// scheduleApply updates the debounce state and (re)starts the timer.
func scheduleApply(state *debounceState, file string, applyCh chan string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.lastFile = file
	state.generation++

	currentGen := state.generation

	if state.timer != nil {
		state.timer.Stop()
	}

	state.timer = time.AfterFunc(debounceInterval, func() {
		enqueueIfCurrent(state, currentGen, applyCh)
	})
}

// enqueueIfCurrent checks whether the generation is still current and, if so,
// coalesces any stale pending apply and enqueues the latest file.
func enqueueIfCurrent(state *debounceState, expectedGen uint64, applyCh chan string) {
	state.mutex.Lock()

	if expectedGen != state.generation {
		state.mutex.Unlock()

		return
	}

	file := state.lastFile
	state.mutex.Unlock()

	// Coalesce: drain any stale pending apply, then enqueue latest.
	// NOTE: safe because the generation guard above ensures only one
	// timer callback is active at any time (single sender).
	select {
	case <-applyCh:
	default:
	}

	select {
	case applyCh <- file:
	default:
	}
}

// executeAndReportApply runs kubectl apply against the given directory and
// prints a timestamped result line with elapsed time. The label parameter
// (e.g. "initial apply", "reconciling") is printed before the apply starts.
// Used directly for the initial full-root sync and called by applyAndReport
// for scoped reconciles, keeping timing and formatting in one place.
func executeAndReportApply(ctx context.Context, cmd *cobra.Command, dir, label string) {
	if ctx.Err() != nil {
		return
	}

	timestamp := time.Now().Format("15:04:05")
	cmd.PrintErrf("[%s] %s\n", timestamp, label)

	start := time.Now()
	applyErr := runKubectlApply(ctx, cmd, dir)
	elapsed := time.Since(start)

	timestamp = time.Now().Format("15:04:05")

	if applyErr != nil {
		cmd.PrintErrf(
			"[%s] ✗ apply failed (%s): %v\n\n",
			timestamp,
			formatElapsed(elapsed),
			applyErr,
		)
	} else {
		cmd.PrintErrf("[%s] ✓ apply succeeded (%s)\n\n", timestamp, formatElapsed(elapsed))
	}
}

// applyAndReport runs kubectl apply and prints a timestamped status line with
// elapsed time. It scopes the apply to the nearest Kustomization subtree
// containing the changed file, falling back to a full reconcile when the
// change is at the root level or no kustomization.yaml boundary is found.
//
// When a Flux reconciler is available, it additionally triggers selective
// Flux Kustomization CR reconciliation for the affected subtree. If no
// CRs match the change, the root Kustomization CR is reconciled instead.
// When multiple CRs match (e.g. a parent directory change affects several
// child Kustomizations), all matching CRs are reconciled.
func applyAndReport(
	ctx context.Context,
	cmd *cobra.Command,
	dir, changedFile string,
	fluxReconciler *flux.Reconciler,
	cachedKustomizations *[]flux.KustomizationInfo,
) {
	if ctx.Err() != nil {
		return
	}

	timestamp := time.Now().Format("15:04:05")

	relFile, err := filepath.Rel(dir, changedFile)
	if err != nil {
		relFile = changedFile
	}

	cmd.PrintErrf("[%s] change detected: %s\n", timestamp, relFile)

	applyDir := findKustomizationDir(changedFile, dir)

	label := "reconciling"

	if applyDir != dir {
		relDir, relErr := filepath.Rel(dir, applyDir)
		if relErr != nil {
			relDir = applyDir
		}

		label = "→ reconciling subtree: " + relDir
	}

	executeAndReportApply(ctx, cmd, applyDir, label)

	reconcileFluxSelectively(ctx, cmd, fluxReconciler, applyDir, dir, cachedKustomizations)
}

// formatElapsed formats a duration as a compact human-readable string
// (e.g. "0.3s", "1.2s", "45.0s").
func formatElapsed(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// findKustomizationDir walks up from the changed path to find the nearest
// directory containing a kustomization.yaml. Both changedFile and rootDir are
// normalized to absolute paths before comparison so that mixed relative /
// absolute inputs are handled correctly. If the nearest match is the root
// watch directory or no match is found, rootDir is returned (triggering a full
// reconcile). When changedFile is itself a directory the search starts there
// instead of at its parent.
func findKustomizationDir(changedFile, rootDir string) string {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return rootDir
	}

	absChanged, err := filepath.Abs(changedFile)
	if err != nil {
		return absRoot
	}

	// When the changed path is a directory, start the search there;
	// otherwise start at its parent directory.
	dir := filepath.Dir(absChanged)

	info, statErr := os.Stat(absChanged)
	if statErr == nil && info.IsDir() {
		dir = absChanged
	}

	for {
		kustomizationPath := filepath.Join(dir, kustomizationFileName)

		_, statErr = os.Stat(kustomizationPath)
		if statErr == nil {
			return dir
		}

		// Reached the root watch directory without finding a nested kustomization.
		if dir == absRoot {
			return absRoot
		}

		parent := filepath.Dir(dir)

		// Reached the filesystem root without finding anything.
		if parent == dir {
			return absRoot
		}

		dir = parent
	}
}

// tryCreateFluxReconciler attempts to create a Flux reconciler using the
// current kubeconfig. Returns nil if the reconciler cannot be created
// (e.g., no kubeconfig, cluster unreachable). The caller should treat
// a nil return as "Flux is unavailable; skip selective reconciliation".
func tryCreateFluxReconciler() *flux.Reconciler {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	if kubeconfigPath == "" {
		return nil
	}

	r, err := flux.NewReconciler(kubeconfigPath)
	if err != nil {
		return nil
	}

	return r
}

// reconcileFluxSelectively triggers Flux Kustomization CR reconciliation
// scoped to the affected subtree. It uses a cached list of Kustomization CRs
// to avoid an API round-trip on every apply, refreshing the cache on the first
// call or when a previous list returned an error.
//
// When the reconciler is nil or Flux is not available, the function silently
// returns. Root-level or unmappable changes reconcile the root Kustomization
// CR. When multiple CRs match (e.g. a parent directory change affects several
// child Kustomizations), all matching CRs are reconciled individually.
func reconcileFluxSelectively(
	ctx context.Context,
	cmd *cobra.Command,
	reconciler *flux.Reconciler,
	applyDir, rootDir string,
	cachedKustomizations *[]flux.KustomizationInfo,
) {
	if reconciler == nil || ctx.Err() != nil {
		return
	}

	// Populate cache on first call or refresh on previous list error.
	if len(*cachedKustomizations) == 0 {
		kustomizations, err := reconciler.ListKustomizationPaths(ctx)
		if err != nil || len(kustomizations) == 0 {
			return
		}

		*cachedKustomizations = kustomizations
	}

	// Root-level change or no subtree match: reconcile the root CR.
	if applyDir == rootDir {
		reconcileRootKustomization(ctx, cmd, reconciler, "root")

		return
	}

	matches := matchFluxKustomizations(applyDir, rootDir, *cachedKustomizations)

	if len(matches) == 0 {
		reconcileRootKustomization(ctx, cmd, reconciler, "root fallback")

		return
	}

	reconcileMatchedKustomizations(ctx, cmd, reconciler, matches)
}

// reconcileRootKustomization triggers reconciliation of the root Kustomization
// CR and prints a timestamped status line. The label parameter (e.g. "root",
// "root fallback") is included in the output to indicate the trigger reason.
func reconcileRootKustomization(
	ctx context.Context,
	cmd *cobra.Command,
	reconciler *flux.Reconciler,
	label string,
) {
	timestamp := time.Now().Format("15:04:05")

	err := reconciler.TriggerKustomizationReconciliation(ctx)
	if err != nil {
		cmd.PrintErrf(
			"[%s] ⚠ flux reconcile (%s): %v\n",
			timestamp, label, err,
		)
	} else {
		cmd.PrintErrf(
			"[%s] ↻ flux: reconciled root kustomization (%s)\n",
			timestamp, label,
		)
	}
}

// reconcileMatchedKustomizations triggers reconciliation of each named
// Kustomization CR and prints a timestamped status line per CR.
func reconcileMatchedKustomizations(
	ctx context.Context,
	cmd *cobra.Command,
	reconciler *flux.Reconciler,
	matches []string,
) {
	timestamp := time.Now().Format("15:04:05")

	for _, name := range matches {
		err := reconciler.TriggerNamedKustomizationReconciliation(ctx, name)
		if err != nil {
			cmd.PrintErrf("[%s] ⚠ flux reconcile %q: %v\n", timestamp, name, err)
		} else {
			cmd.PrintErrf("[%s] ↻ flux: reconciled kustomization %q\n", timestamp, name)
		}
	}
}

// matchFluxKustomizations maps a changed directory (absolute path) to the
// Flux Kustomization CR(s) whose spec.path matches. A match occurs when
// the normalized relative path of the changed directory equals or is a
// parent/child of the CR's spec.path. Returns nil when no CRs match.
func matchFluxKustomizations(
	changedDir, rootDir string,
	kustomizations []flux.KustomizationInfo,
) []string {
	relDir, err := filepath.Rel(rootDir, changedDir)
	if err != nil {
		return nil
	}

	relDir = normalizeFluxPath(relDir)
	if relDir == "" {
		return nil
	}

	var matches []string

	for _, kustomization := range kustomizations {
		ksPath := normalizeFluxPath(kustomization.Path)
		if ksPath == "" {
			continue
		}

		if ksPath == relDir ||
			strings.HasPrefix(ksPath, relDir+"/") ||
			strings.HasPrefix(relDir, ksPath+"/") {
			matches = append(matches, kustomization.Name)
		}
	}

	return matches
}

// normalizeFluxPath strips leading "./" and cleans the path, converting
// OS-specific separators to forward slashes so prefix checks work
// consistently across platforms. Returns "" for paths that resolve to "."
// (root-level).
func normalizeFluxPath(path string) string {
	path = strings.TrimPrefix(path, "./")
	path = filepath.ToSlash(filepath.Clean(path))

	if path == "." {
		return ""
	}

	return path
}

// runKubectlApply executes kubectl apply -k against the provided directory,
// which may be the root watch directory or a scoped Kustomization subtree.
// The provided context is forwarded to the cobra command so that Ctrl+C
// (which cancels ctx) also terminates an in-flight apply promptly.
func runKubectlApply(ctx context.Context, cmd *cobra.Command, dir string) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    cmd.OutOrStdout(),
		ErrOut: cmd.ErrOrStderr(),
	})

	applyCmd := client.CreateApplyCommand(kubeconfigPath)
	applyCmd.SetArgs([]string{"-k", dir})
	applyCmd.SetOut(cmd.OutOrStdout())
	applyCmd.SetErr(cmd.ErrOrStderr())

	err := applyCmd.ExecuteContext(ctx)
	if err != nil {
		return fmt.Errorf("kubectl apply: %w", err)
	}

	return nil
}

// isRelevantEvent returns true for write, create, remove, and rename events.
func isRelevantEvent(event fsnotify.Event) bool {
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Remove) ||
		event.Has(fsnotify.Rename)
}

// addRecursive walks the directory tree and adds all directories to the watcher.
func addRecursive(watcher *fsnotify.Watcher, root string) error {
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			watchErr := watcher.Add(path)
			if watchErr != nil {
				return fmt.Errorf("watch %q: %w", path, watchErr)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk directory %q: %w", root, err)
	}

	return nil
}

// tryAddDirectory attempts to add a path to the watcher if it is a directory.
func tryAddDirectory(watcher *fsnotify.Watcher, path string, cmd *cobra.Command) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	if info.IsDir() {
		addErr := addRecursive(watcher, path)
		if addErr != nil {
			cmd.PrintErrf("⚠️  failed to watch new directory %s: %v\n", path, addErr)
		}
	}
}
