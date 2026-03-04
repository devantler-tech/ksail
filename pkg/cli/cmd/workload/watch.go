package workload

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
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

// NewWatchCmd creates the workload watch command.
func NewWatchCmd() *cobra.Command {
	var pathFlag string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch for file changes and auto-apply workloads",
		Long: `Watch a directory for file changes and automatically apply workloads.

When files in the watched directory are created, modified, or deleted,
the command debounces changes (~500ms) then runs the equivalent of
'ksail workload apply -k <path>' to reconcile the cluster.

Each reconcile prints a timestamped status line showing the changed file
and the outcome (success or failure). Press Ctrl+C to stop the watcher.

Examples:
  # Watch the default k8s/ directory
  ksail workload watch

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

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runWatch(cmd, pathFlag)
	}

	return cmd
}

// runWatch starts the file watcher loop.
func runWatch(cmd *cobra.Command, pathFlag string) error {
	cmdCtx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	watchDir := resolveWatchDir(cmdCtx.ClusterCfg, pathFlag)

	// Verify the directory exists.
	info, err := os.Stat(watchDir)
	if err != nil {
		return fmt.Errorf("access watch directory %q: %w", watchDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("watch path %q is not a directory", watchDir)
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

	return watchLoop(cmd.Context(), cmd, absDir)
}

// resolveWatchDir determines the directory to watch from flag, config, or default.
func resolveWatchDir(cfg *v1alpha1.Cluster, pathFlag string) string {
	if dir := strings.TrimSpace(pathFlag); dir != "" {
		return dir
	}

	if dir := strings.TrimSpace(cfg.Spec.Workload.SourceDirectory); dir != "" {
		return dir
	}

	return v1alpha1.DefaultSourceDirectory
}

// watchLoop sets up the fsnotify watcher and runs the debounced apply loop.
func watchLoop(ctx context.Context, cmd *cobra.Command, dir string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create file watcher: %w", err)
	}

	defer watcher.Close()

	// Add all directories recursively.
	if err := addRecursive(watcher, dir); err != nil {
		return err
	}

	// Set up signal handling for graceful shutdown.
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return eventLoop(sigCtx, cmd, watcher, dir)
}

// eventLoop processes fsnotify events with debouncing.
func eventLoop(
	ctx context.Context,
	cmd *cobra.Command,
	watcher *fsnotify.Watcher,
	dir string,
) error {
	var (
		debounceTimer *time.Timer
		mu            sync.Mutex
		lastFile      string
		generation    uint64
	)

	defer func() {
		mu.Lock()
		defer mu.Unlock()

		// Increment generation so any pending timer callback becomes a no-op.
		generation++

		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			cmd.PrintErrln("\n✋ watcher stopped")

			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			if !isRelevantEvent(event) {
				continue
			}

			// If a new directory was created, watch it too.
			if event.Has(fsnotify.Create) {
				tryAddDirectory(watcher, event.Name, cmd)
			}

			mu.Lock()
			lastFile = event.Name
			generation++

			currentGen := generation

			if debounceTimer != nil {
				debounceTimer.Stop()
			}

			debounceTimer = time.AfterFunc(debounceInterval, func() {
				mu.Lock()
				if currentGen != generation {
					mu.Unlock()

					return
				}

				file := lastFile
				mu.Unlock()

				applyAndReport(ctx, cmd, dir, file)
			})
			mu.Unlock()

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}

			cmd.PrintErrf("⚠️  watcher error: %v\n", watchErr)
		}
	}
}

// applyAndReport runs kubectl apply and prints a timestamped status line.
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

	applyErr := runKubectlApply(cmd, dir)

	timestamp = time.Now().Format("15:04:05")

	if applyErr != nil {
		cmd.PrintErrf("[%s] ✗ apply failed: %v\n", timestamp, applyErr)
	} else {
		cmd.PrintErrf("[%s] ✓ apply succeeded\n", timestamp)
	}
}

// runKubectlApply executes kubectl apply -k against the watched directory.
func runKubectlApply(cmd *cobra.Command, dir string) error {
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

	return applyCmd.Execute()
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
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if watchErr := watcher.Add(path); watchErr != nil {
				return fmt.Errorf("watch %q: %w", path, watchErr)
			}
		}

		return nil
	})
}

// tryAddDirectory attempts to add a path to the watcher if it is a directory.
func tryAddDirectory(watcher *fsnotify.Watcher, path string, cmd *cobra.Command) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	if info.IsDir() {
		if addErr := addRecursive(watcher, path); addErr != nil {
			cmd.PrintErrf("⚠️  failed to watch new directory %s: %v\n", path, addErr)
		}
	}
}
