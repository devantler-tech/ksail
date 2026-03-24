package workload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
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

	absDir, err := filepath.Abs(watchDir)
	if err != nil {
		return fmt.Errorf("resolve absolute path for %q: %w", watchDir, err)
	}

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "👁️",
		Content: "Watching for changes...",
		Writer:  cmd.OutOrStdout(),
	})

	cmd.PrintErrf("  watching: %s\n", absDir)
	cmd.PrintErrf("  press Ctrl+C to stop\n\n")

	return watchLoop(cmd.Context(), cmd, absDir, initialApply)
}

// watchLoop sets up the fsnotify watcher and runs the debounced apply loop.
// When initialApply is true, a full apply of the watch root is performed
// after the event loop goroutine is started, so watcher events are consumed
// immediately and not dropped or buffered during the initial apply. Ctrl+C
// cancels both the initial apply and the event loop via the shared sigCtx.
func watchLoop(ctx context.Context, cmd *cobra.Command, dir string, initialApply bool) error {
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
		errCh <- eventLoop(sigCtx, cmd, watcher, dir)
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
) error {
	state := &debounceState{}

	// applyCh serializes applies.  Capacity 1 ensures at most one apply is
	// pending at any time; coalescing replaces a queued entry with the latest.
	applyCh := make(chan string, 1)

	// Single worker: runs applies one at a time, stops when ctx is cancelled.
	go applyWorker(ctx, cmd, dir, applyCh)

	defer cancelPendingDebounce(state)

	return dispatchEvents(ctx, cmd, watcher, state, applyCh)
}

// applyWorker runs applies one at a time, stopping when ctx is cancelled.
func applyWorker(ctx context.Context, cmd *cobra.Command, dir string, applyCh <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case file, ok := <-applyCh:
			if !ok {
				return
			}

			applyAndReport(ctx, cmd, dir, file)
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
func applyAndReport(ctx context.Context, cmd *cobra.Command, dir, changedFile string) {
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
