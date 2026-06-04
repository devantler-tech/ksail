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

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

var errNotDirectory = errors.New("watch path is not a directory")

// watchCmdLong is the long description for the watch subcommand.
const watchCmdLong = `Watch a directory for file changes and automatically apply workloads.

When files in the watched directory are created, modified, or deleted,
the command debounces changes (~500ms) then scopes the apply to the
nearest directory containing a kustomization file recognized by kubectl
(kustomization.yaml, kustomization.yml, or Kustomization), walking up
from the changed file to the watch root. If no kustomization boundary is
found, or the boundary is the watch root, it applies the full root
directory. This scoping ensures only the affected Kustomize layer is
re-applied, making iteration faster in monorepo-style layouts.

Each reconcile prints a timestamped status line showing the changed file,
the outcome (success or failure), and the elapsed time for the apply.
Press Ctrl+C to stop the watcher.

Use --initial-apply to synchronize the cluster with the current state of
the watch directory before entering the watch loop. This is useful after
editing manifests offline or when starting a fresh session.

Use --hook to run shell commands before each apply (e.g. docker build).
Hooks execute sequentially; if any hook fails the apply is skipped for
that cycle. Hooks can also be configured in ksail.yaml under
spec.workload.watch.hooks. CLI --hook flags are appended after config hooks.

Examples:
  # Watch the default k8s/ directory
  ksail workload watch

  # Watch and apply once on startup before entering the loop
  ksail workload watch --initial-apply

  # Watch a custom directory
  ksail workload watch --path=./manifests

  # Run a build before each apply
  ksail workload watch --hook "docker build -t myapp:latest ."

  # Chain multiple hooks
  ksail workload watch --hook "make generate" --hook "docker build -t myapp ."`

// debounceInterval is the time to wait after the last file event before
// triggering an apply. This prevents redundant reconciles during batch saves.
const debounceInterval = 500 * time.Millisecond

// pollInterval is the time between file modification time scans. Acts as a
// safety net for environments where inotify may miss events (CI runners under
// high I/O load, editors using atomic save via create+rename, etc.).
const pollInterval = 3 * time.Second

// fileSnapshot maps file paths to their last-known modification time.
// Used by the polling fallback to detect changes missed by fsnotify.
type fileSnapshot map[string]time.Time

// debounceState holds the mutable state shared between the event loop and
// debounce timer callbacks.
type debounceState struct {
	timer      *time.Timer
	mutex      sync.Mutex
	lastFile   string
	generation uint64
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

// NewWatchCmd creates the workload watch command.
func NewWatchCmd() *cobra.Command {
	var (
		pathFlag     string
		initialApply bool
		debugFlag    bool
		hookFlags    []string
	)

	cmd := &cobra.Command{
		Use:          "watch",
		Short:        "Watch for file changes and auto-apply workloads",
		Long:         watchCmdLong,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
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

	cmd.Flags().BoolVar(
		&debugFlag, "debug", false,
		"Show diagnostic output for file events and polling (useful for troubleshooting watch behavior)",
	)

	cmd.Flags().StringArrayVar(
		&hookFlags, "hook", nil,
		"Shell command to run before each apply (repeatable; appended after spec.workload.watch.hooks)",
	)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runWatch(cmd, pathFlag, initialApply, debugFlag, hookFlags)
	}

	return cmd
}

// validateWatchDir checks that dir exists and is a directory.
func validateWatchDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("access watch directory %q: %w", dir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%q: %w", dir, errNotDirectory)
	}

	return nil
}

// runWatch starts the file watcher loop.
func runWatch(
	cmd *cobra.Command,
	pathFlag string,
	initialApply bool,
	debug bool,
	hookFlags []string,
) error {
	// The root-level error executor captures stderr into a buffer for error
	// aggregation.  For long-running commands like watch the buffer is never
	// flushed, making all feedback invisible.  Override with real stderr so
	// that watcher diagnostics (change detected, apply results) appear in the
	// terminal and in CI logs.
	cmd.SetErr(os.Stderr)

	// Validate an explicitly supplied --path before loading config so that a
	// missing or invalid path is reported immediately (before any expensive
	// config loading or cluster connection).  The CI contract test
	// (ksail-test-workload-watch) relies on this early-exit behaviour.
	if dir := strings.TrimSpace(pathFlag); dir != "" {
		err := validateWatchDir(dir)
		if err != nil {
			return err
		}
	}

	cmdCtx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	watchDir := resolveSourceDir(cmdCtx.ClusterCfg, pathFlag)

	err = validateWatchDir(watchDir)
	if err != nil {
		return err
	}

	// Canonicalize the watch directory (resolve symlinks + absolute) so that
	// file events are matched against the real directory and symlink-escape
	// attacks are prevented in CI pipelines processing external manifests.
	absDir, err := fsutil.EvalCanonicalPath(watchDir)
	if err != nil {
		return fmt.Errorf("resolve watch directory %q: %w", watchDir, err)
	}

	// Merge hooks: config hooks first, then CLI --hook flags appended.
	// Allocate a new slice to avoid mutating the config's backing array.
	configHooks := cmdCtx.ClusterCfg.Spec.Workload.Watch.Hooks
	hooks := make([]string, 0, len(configHooks)+len(hookFlags))
	hooks = append(hooks, configHooks...)
	hooks = append(hooks, hookFlags...)

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

	if len(hooks) > 0 {
		cmd.PrintErrf("  hooks:    %d configured\n", len(hooks))
	}

	cmd.PrintErrf("  press Ctrl+C to stop\n\n")

	return watchLoop(cmd.Context(), cmd, absDir, initialApply, fluxReconciler, debug, hooks)
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
	debug bool,
	hooks []string,
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
		errCh <- eventLoop(sigCtx, cmd, watcher, dir, fluxReconciler, debug, hooks)
	}()

	if initialApply {
		executeAndReportApply(sigCtx, cmd, dir, "initial apply", hooks)
	}

	// Wait for the event loop to complete and propagate its error.
	return <-errCh
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
	debug bool,
	hooks []string,
) error {
	state := &debounceState{}

	// applyCh serializes applies.  Capacity 1 ensures at most one apply is
	// pending at any time; coalescing replaces a queued entry with the latest.
	applyCh := make(chan string, 1)

	// Single worker: runs applies one at a time, stops when ctx is cancelled.
	go applyWorker(ctx, cmd, dir, applyCh, fluxReconciler, hooks)

	// Polling fallback: periodically scan for modification time changes to
	// catch events missed by fsnotify (CI runners, atomic-save editors).
	// Runs independently from the fsnotify debounce state so that fsnotify
	// events cannot invalidate polling-detected changes.
	go pollForChanges(ctx, dir, applyCh, debug)

	defer cancelPendingDebounce(state)

	return dispatchEvents(ctx, cmd, watcher, state, applyCh, debug)
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
	hooks []string,
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

			applyAndReport(ctx, cmd, dir, file, fluxReconciler, &cachedKustomizations, hooks)
		}
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
	debug bool,
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

			handleFileEvent(event, watcher, cmd, state, applyCh, debug)

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
	debug bool,
) {
	if !isRelevantEvent(event) {
		return
	}

	if debug {
		fmt.Fprintf(os.Stderr, "  fsnotify: %s %s\n", event.Op, event.Name)
	}

	// If a new directory was created, watch it too.
	if event.Has(fsnotify.Create) {
		tryAddDirectory(watcher, event.Name, cmd)
	}

	scheduleApply(state, event.Name, applyCh)
}

// executeAndReportApply runs pre-apply hooks and kubectl apply against the
// given directory, printing a timestamped result line with elapsed time.
// The label parameter (e.g. "initial apply", "reconciling") is printed
// before the apply starts. If any hook fails, the apply is skipped.
// Used directly for the initial full-root sync and called by applyAndReport
// for scoped reconciles, keeping timing and formatting in one place.
func executeAndReportApply(
	ctx context.Context,
	cmd *cobra.Command,
	dir, label string,
	hooks []string,
) {
	if ctx.Err() != nil {
		return
	}

	timestamp := time.Now().Format("15:04:05")
	cmd.PrintErrf("[%s] %s\n", timestamp, label)

	start := time.Now()

	// Run pre-apply hooks; skip the apply if any hook fails.
	if len(hooks) > 0 {
		cmd.PrintErrf("[%s] running %d hook(s)\n", timestamp, len(hooks))

		hookErr := runHooks(ctx, cmd, hooks)
		if hookErr != nil {
			elapsed := time.Since(start)
			timestamp = time.Now().Format("15:04:05")
			cmd.PrintErrf(
				"[%s] ✗ hook failed, apply skipped (%s): %v\n\n",
				timestamp,
				formatElapsed(elapsed),
				hookErr,
			)

			return
		}
	}

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
	hooks []string,
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

	executeAndReportApply(ctx, cmd, applyDir, label, hooks)

	reconcileFluxSelectively(ctx, cmd, fluxReconciler, applyDir, dir, cachedKustomizations)
}

// formatElapsed formats a duration as a compact human-readable string
// (e.g. "0.3s", "1.2s", "45.0s").
func formatElapsed(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// findKustomizationDir walks up from the changed path to find the nearest
// directory containing a kustomization file recognized by kubectl
// (kustomization.yaml, kustomization.yml, or Kustomization). Both changedFile
// and rootDir are normalized to absolute paths before comparison so that mixed
// relative / absolute inputs are handled correctly. If the nearest match is
// the root watch directory or no match is found, rootDir is returned
// (triggering a full reconcile). When changedFile is itself a directory the
// search starts there instead of at its parent.
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
		if hasKustomizationFile(dir) {
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
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(nil)
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

// hasKustomizationFile reports whether dir contains a regular kustomization
// file recognized by kubectl (kustomization.yaml, kustomization.yml, or
// Kustomization). Non-ErrNotExist errors (e.g., permission denied) are treated
// as a positive match so that transient stat failures do not silently switch
// the apply mode from -k to -f --recursive.
func hasKustomizationFile(dir string) bool {
	for _, name := range kustomizationFileNames {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil {
			if info.Mode().IsRegular() {
				return true
			}

			continue
		}

		if !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}

	return false
}

// runKubectlApply executes kubectl apply against the provided directory,
// which may be the root watch directory or a scoped Kustomization subtree.
// When the directory contains a kustomization file recognized by kubectl
// (kustomization.yaml, kustomization.yml, or Kustomization), it applies
// using -k (kustomize mode). Otherwise it falls back to -f with --recursive
// to apply all manifest files in the directory tree.
// The provided context is forwarded to the cobra command so that Ctrl+C
// (which cancels ctx) also terminates an in-flight apply promptly.
func runKubectlApply(ctx context.Context, cmd *cobra.Command, dir string) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    cmd.OutOrStdout(),
		ErrOut: cmd.ErrOrStderr(),
	})

	applyCmd := client.CreateApplyCommand(kubeconfigPath)

	var mode string

	if hasKustomizationFile(dir) {
		applyCmd.SetArgs([]string{"-k", dir})

		mode = "-k"
	} else {
		applyCmd.SetArgs([]string{"-f", dir, "--recursive"})

		mode = "-f --recursive"
	}

	applyCmd.SetOut(cmd.OutOrStdout())
	applyCmd.SetErr(cmd.ErrOrStderr())

	err := kubectl.ExecuteSafely(ctx, applyCmd)
	if err != nil {
		return fmt.Errorf("kubectl apply (%s): %w", mode, err)
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

// buildFileSnapshot walks the directory tree and records modification times
// for all regular files. Uses os.Stat instead of d.Info() to avoid stale
// cached stat data when files are replaced via rename (e.g. sed -i).
func buildFileSnapshot(dir string) fileSnapshot {
	snap := make(fileSnapshot)

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil //nolint:nilerr // skip non-regular entries and stat errors
		}

		snap[path] = info.ModTime()

		return nil
	})

	return snap
}

// detectChangedFile scans the directory for a file whose modification time
// differs from the snapshot. Returns the first changed file path found and
// updates the snapshot in place. Returns "" if no changes are detected.
func detectChangedFile(dir string, snapshot fileSnapshot) string {
	changed := scanForModifiedFiles(dir, snapshot)
	deleted := scanForDeletedFiles(snapshot)

	if changed != "" {
		return changed
	}

	return deleted
}

// scanForModifiedFiles walks the directory tree and returns the first file
// whose modification time differs from the snapshot. Updates the snapshot
// in place for all changed files encountered during the walk.
func scanForModifiedFiles(dir string, snapshot fileSnapshot) string {
	var changed string

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil //nolint:nilerr // skip non-regular entries and stat errors
		}

		modTime := info.ModTime()
		if prev, ok := snapshot[path]; !ok || !modTime.Equal(prev) {
			snapshot[path] = modTime

			if changed == "" {
				changed = path
			}
		}

		return nil
	})

	return changed
}

// scanForDeletedFiles checks all snapshot entries and removes any whose
// path is missing or is no longer a regular file. Returns the first
// deleted path found, or "".
func scanForDeletedFiles(snapshot fileSnapshot) string {
	var changed string

	for path := range snapshot {
		info, statErr := os.Lstat(path)

		if statErr != nil || !info.Mode().IsRegular() {
			delete(snapshot, path)

			if changed == "" {
				changed = path
			}
		}
	}

	return changed
}

// pollForChanges periodically scans the watched directory for modified files
// and enqueues applies directly on applyCh. This provides a fallback for
// environments where fsnotify events may be lost (CI runners, atomic-save
// editors using create+rename).
//
// Unlike the fsnotify path, polling bypasses the shared debounce state
// entirely. The polling interval (3s) already provides natural debouncing,
// and a blocking send guarantees at least one apply runs for the detected
// change. A later fsnotify event may still coalesce with it before it is
// applied (enqueueIfCurrent drains applyCh and the apply reflects the
// newest state), so the guarantee is "an apply will happen", not
// one-for-one delivery of each individual poll event.
func pollForChanges(ctx context.Context, dir string, applyCh chan string, debug bool) {
	snapshot := buildFileSnapshot(dir)

	logPollSnapshot(dir, snapshot, debug)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	tickCount := 0

	for {
		select {
		case <-ctx.Done():
			if debug {
				fmt.Fprintf(os.Stderr, "  poll: stopped after %d ticks\n", tickCount)
			}

			return
		case <-ticker.C:
			tickCount++

			if tickCount%5 == 1 {
				logPollTick(tickCount, dir, snapshot, debug)
			}

			if !pollHandleChange(ctx, dir, snapshot, applyCh, tickCount, debug) {
				return
			}
		}
	}
}

// pollHandleChange detects and enqueues a changed file for the apply worker.
// Returns false if the context was cancelled during the blocking send.
func pollHandleChange(
	ctx context.Context,
	dir string,
	snapshot fileSnapshot,
	applyCh chan string,
	tickCount int,
	debug bool,
) bool {
	changed := detectChangedFile(dir, snapshot)
	if changed == "" {
		return true
	}

	if debug {
		fmt.Fprintf(os.Stderr, "  poll: change on tick %d: %s\n", tickCount, changed)
	}

	// Blocking send: guaranteed delivery to the apply worker.
	select {
	case applyCh <- changed:
		if debug {
			fmt.Fprintf(os.Stderr, "  poll: enqueued for apply\n")
		}

		return true
	case <-ctx.Done():
		return false
	}
}

// logPollSnapshot logs the initial snapshot contents (temporary diagnostics).
func logPollSnapshot(dir string, snapshot fileSnapshot, debug bool) {
	if !debug {
		return
	}

	fmt.Fprintf(os.Stderr, "  poll: started, %d files in snapshot\n", len(snapshot))

	for path, modTime := range snapshot {
		rel, _ := filepath.Rel(dir, path)
		fmt.Fprintf(os.Stderr, "  poll:   %s (mod=%s)\n", rel, modTime.Format(time.RFC3339Nano))
	}
}

// logPollTick logs file modTimes vs snapshot on periodic ticks (temporary diagnostics).
func logPollTick(tick int, dir string, snapshot fileSnapshot, debug bool) {
	if !debug {
		return
	}

	fmt.Fprintf(os.Stderr, "  poll: tick %d, scanning %s\n", tick, dir)

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries and directories
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil //nolint:nilerr // skip files that can't be stat'd
		}

		rel, _ := filepath.Rel(dir, path)
		cur := info.ModTime()
		prev, inSnap := snapshot[path]

		switch {
		case !inSnap:
			fmt.Fprintf(os.Stderr, "  poll:   %s NEW mod=%s\n", rel, cur.Format(time.RFC3339Nano))
		case !cur.Equal(prev):
			fmt.Fprintf(os.Stderr, "  poll:   %s CHANGED snap=%s cur=%s\n",
				rel, prev.Format(time.RFC3339Nano), cur.Format(time.RFC3339Nano))
		default:
			fmt.Fprintf(os.Stderr, "  poll:   %s unchanged mod=%s\n",
				rel, cur.Format(time.RFC3339Nano))
		}

		return nil
	})
}
