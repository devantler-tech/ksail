package cluster

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/state"
	"github.com/spf13/cobra"
)

// deleteTimeout is the maximum duration for the auto-delete operation.
const deleteTimeout = 10 * time.Minute

// waitForTTLAndDelete blocks until the TTL duration elapses and then auto-deletes the cluster.
// The wait can be cancelled with SIGINT/SIGTERM, in which case the cluster is left running.
// This implements the ephemeral cluster pattern: after creation, the process stays alive
// and automatically tears down the cluster when the TTL expires.
func waitForTTLAndDelete(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	ttl time.Duration,
) error {
	notify.Infof(cmd.OutOrStdout(),
		"cluster will auto-destroy in %s (press Ctrl+C to cancel)", ttl)

	// Create a context that is cancelled on SIGINT/SIGTERM and also respects cmd.Context().
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	timer := time.NewTimer(ttl)
	defer timer.Stop()

	select {
	case <-timer.C:
		return autoDeleteCluster(cmd, clusterName, clusterCfg)
	case <-ctx.Done():
		notify.Infof(cmd.OutOrStdout(),
			"TTL wait cancelled; cluster %q will remain running", clusterName)

		return nil
	}
}

// autoDeleteCluster performs an automatic cluster deletion after TTL expiry.
// It creates a minimal provisioner based on distribution and provider info
// from the original cluster config and deletes the cluster.
func autoDeleteCluster(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
) error {
	notify.Infof(cmd.OutOrStdout(),
		"TTL expired; auto-destroying cluster %q...", clusterName)

	info := &clusterdetector.Info{
		ClusterName:  clusterName,
		Distribution: clusterCfg.Spec.Cluster.Distribution,
		Provider:     clusterCfg.Spec.Cluster.Provider,
	}

	provisioner, err := createDeleteProvisioner(info, clusterCfg.Spec.Cluster.Omni)
	if err != nil {
		return fmt.Errorf("TTL auto-delete: failed to create provisioner: %w", err)
	}

	deleteCtx, cancel := context.WithTimeout(cmd.Context(), deleteTimeout)
	defer cancel()

	err = provisioner.Delete(deleteCtx, clusterName)
	if err != nil {
		return fmt.Errorf("TTL auto-delete failed: %w", err)
	}

	// Clean up persisted state (spec + TTL).
	// Best-effort: warn on failure rather than blocking success.
	stateErr := state.DeleteClusterState(clusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"failed to clean up cluster state: %v", stateErr)
	}

	notify.Successf(cmd.OutOrStdout(),
		"cluster %q auto-destroyed after TTL expiry", clusterName)

	return nil
}
