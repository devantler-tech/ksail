package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

// NewStartCmd creates and returns the start command.
func NewStartCmd(_ any) *cobra.Command {
	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use:          "start",
		Short:        "Start a stopped cluster",
		Long:         startLongDesc,
		TitleEmoji:   "▶️",
		TitleContent: "Start cluster...",
		Activity:     "starting",
		Success:      "cluster started",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			return provisioner.Start(ctx, clusterName)
		},
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}

	return cmd
}

const stopLongDesc = `Stop a running Kubernetes cluster.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

Supported distributions are automatically detected from existing clusters.`

// NewStopCmd creates and returns the stop command.
func NewStopCmd(_ any) *cobra.Command {
	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use:          "stop",
		Short:        "Stop a running cluster",
		Long:         stopLongDesc,
		TitleEmoji:   "🛑",
		TitleContent: "Stop cluster...",
		Activity:     "stopping",
		Success:      "cluster stopped",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			return provisioner.Stop(ctx, clusterName)
		},
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}

	return cmd
}

// ErrContextNotFound is returned when the specified cluster does not have a matching context in the kubeconfig.
var ErrContextNotFound = errors.New("no matching context found for cluster")

// ErrAmbiguousCluster is returned when multiple distribution contexts match the cluster name.
var ErrAmbiguousCluster = errors.New("ambiguous cluster name")

// ErrNoClusters is returned when no KSail-managed clusters are found in the kubeconfig.
var ErrNoClusters = errors.New("no KSail-managed clusters found in kubeconfig")

// switchHistoryMaxItems is the maximum number of recently-switched clusters to remember.
const switchHistoryMaxItems = 5

// switchHistoryFileName is the file name used to persist switch history under ~/.ksail/.
const switchHistoryFileName = "switch-history.json"

// switchHistoryDirPerms is the permission mode for the ~/.ksail/ directory.
const switchHistoryDirPerms = 0o700

// switchHistoryFilePerms is the permission mode for the switch-history.json file.
const switchHistoryFilePerms = 0o600

// switchHistory is the JSON representation persisted to ~/.ksail/switch-history.json.
type switchHistory struct {
	Recent []string `json:"recent"`
}

// switchHistoryPath returns the path to the switch history file (~/.ksail/switch-history.json).
func switchHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}

	return filepath.Join(home, ".ksail", switchHistoryFileName), nil
}

// loadSwitchHistory reads the recent-cluster list from disk.
// Returns nil if the file does not exist or cannot be parsed.
//
//nolint:gosec // G304: path is derived from os.UserHomeDir() with a fixed suffix
func loadSwitchHistory() []string {
	path, err := switchHistoryPath()
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var history switchHistory

	err = json.Unmarshal(data, &history)
	if err != nil {
		return nil
	}

	return history.Recent
}

// saveToSwitchHistory prepends name to the recent list, deduplicates, caps at
// switchHistoryMaxItems, and writes to disk. Errors are silently discarded so that
// a failed history write never blocks the user's context switch.
func saveToSwitchHistory(name string) {
	path, err := switchHistoryPath()
	if err != nil {
		return
	}

	existing := loadSwitchHistory()

	seen := map[string]struct{}{name: {}}
	updated := []string{name}

	for _, n := range existing {
		if _, dup := seen[n]; !dup {
			seen[n] = struct{}{}
			updated = append(updated, n)

			if len(updated) >= switchHistoryMaxItems {
				break
			}
		}
	}

	h := switchHistory{Recent: updated}

	data, err := json.Marshal(h)
	if err != nil {
		return
	}

	mkErr := os.MkdirAll(filepath.Dir(path), switchHistoryDirPerms)
	if mkErr != nil {
		return
	}

	_ = fsutil.AtomicWriteFile(path, data, switchHistoryFilePerms)
}

// switchKubeconfigFileMode is the file mode for kubeconfig files.
const switchKubeconfigFileMode = 0o600

const switchLongDesc = `Switch the active kubeconfig context to the named cluster.

This command accepts a cluster name and automatically resolves it to the
correct kubeconfig context by checking all supported distribution prefixes
(kind-, k3d-, admin@, vcluster-docker_).

If multiple distributions have contexts for the same cluster name, the
command returns an error listing the matching contexts.

The kubeconfig is resolved in the following priority order:
  1. From KUBECONFIG environment variable
  2. From ksail.yaml config file (if present)
  3. Defaults to ~/.kube/config

When called without arguments, an interactive picker is shown
to select from available clusters. Recently-switched clusters
appear at the top of the list (up to the last 5), followed by
the remaining clusters in alphabetical order. The history is
persisted to ~/.ksail/switch-history.json.

In the picker, press '/' to enter filter mode and type to narrow
the list by keyword (case-insensitive). Press Esc to exit filter mode.

Examples:
  # Switch to a Vanilla (Kind) cluster named "dev"
  ksail cluster switch dev

  # Switch to a cluster named "staging"
  ksail cluster switch staging

  # Select a cluster interactively (recent clusters shown first)
  ksail cluster switch`
