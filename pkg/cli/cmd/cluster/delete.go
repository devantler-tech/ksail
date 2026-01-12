package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates and returns the delete command.
// Delete uses context-based detection to determine the cluster distribution and provider,
// requiring only --kubeconfig, --context, and --delete-storage flags.
func NewDeleteCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	var (
		contextFlag    string
		kubeconfigFlag string
		deleteStorage  bool
	)

	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Destroy a cluster",
		Long:          `Destroy a cluster.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeleteAction(
				cmd,
				runtimeContainer,
				kubeconfigFlag,
				contextFlag,
				deleteStorage,
			)
		},
	}

	cmd.Flags().StringVarP(
		&contextFlag,
		"context",
		"c",
		"",
		"Kubernetes context to target (defaults to current context)",
	)

	cmd.Flags().StringVar(
		&kubeconfigFlag,
		"kubeconfig",
		"",
		"Path to kubeconfig file (defaults to $KUBECONFIG or ~/.kube/config)",
	)

	cmd.Flags().BoolVar(
		&deleteStorage,
		"delete-storage",
		false,
		"Delete storage volumes when cleaning up (registry volumes for Docker, block storage for Hetzner)",
	)

	return cmd
}

// runDeleteAction executes the cluster deletion with registry cleanup.
func runDeleteAction(
	cmd *cobra.Command,
	runtimeContainer *runtime.Runtime,
	kubeconfigPath string,
	contextFlag string,
	deleteStorage bool,
) error {
	// Wrap output with StageSeparatingWriter for automatic stage separation
	stageWriter := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
	cmd.SetOut(stageWriter)

	// Get timer from runtime container using Invoke
	var tmr timer.Timer

	if runtimeContainer != nil {
		_ = runtimeContainer.Invoke(func(injector runtime.Injector) error {
			var err error

			tmr, err = runtime.ResolveTimer(injector)

			return err
		})
	}

	if tmr != nil {
		tmr.Start()
	}

	// Detect cluster info from kubeconfig
	clusterInfo, err := lifecycle.DetectClusterInfo(kubeconfigPath, contextFlag)
	if err != nil {
		return fmt.Errorf("failed to detect cluster: %w", err)
	}

	// Create provisioner for the detected distribution
	provisioner, err := createDeleteProvisioner(clusterInfo)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// For Talos, pre-discover registries before deletion (network is destroyed during delete)
	var preDiscovered *mirrorregistry.DiscoveredRegistries
	if clusterInfo.Distribution == v1alpha1.DistributionTalos {
		preDiscovered = discoverRegistriesBeforeDelete(cmd, clusterInfo)
	}

	// Delete the cluster
	err = executeDelete(cmd, tmr, provisioner, clusterInfo)
	if err != nil {
		return err
	}

	// Cleanup registries after cluster deletion
	cleanupRegistriesAfterDelete(cmd, tmr, clusterInfo, deleteStorage, preDiscovered)

	return nil
}

// createDeleteProvisioner creates the appropriate provisioner for cluster deletion.
// It first checks for test overrides, then falls back to creating a minimal provisioner.
//
//nolint:ireturn // Provisioner interface is required
func createDeleteProvisioner(
	clusterInfo *lifecycle.ClusterInfo,
) (clusterprovisioner.ClusterProvisioner, error) {
	// Check for test factory override
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		provisioner, _, err := factoryOverride.Create(context.Background(), nil)
		if err != nil {
			return nil, fmt.Errorf("factory override failed: %w", err)
		}

		return provisioner, nil
	}

	return lifecycle.CreateMinimalProvisioner(clusterInfo)
}

// discoverRegistriesBeforeDelete discovers registries connected to the cluster network.
// This must be called BEFORE cluster deletion for Talos, as the network is destroyed during delete.
func discoverRegistriesBeforeDelete(
	cmd *cobra.Command,
	clusterInfo *lifecycle.ClusterInfo,
) *mirrorregistry.DiscoveredRegistries {
	cleanupDeps := getCleanupDeps()

	return mirrorregistry.DiscoverRegistriesByNetwork(
		cmd,
		clusterInfo.Distribution,
		clusterInfo.ClusterName,
		cleanupDeps,
	)
}

// executeDelete performs the cluster deletion operation.
func executeDelete(
	cmd *cobra.Command,
	tmr timer.Timer,
	provisioner clusterprovisioner.ClusterProvisioner,
	clusterInfo *lifecycle.ClusterInfo,
) error {
	if tmr != nil {
		tmr.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Delete cluster...",
		Emoji:   "🗑️",
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type: notify.ActivityType,
		Content: fmt.Sprintf(
			"deleting %s cluster '%s'",
			clusterInfo.Distribution,
			clusterInfo.ClusterName,
		),
		Writer: cmd.OutOrStdout(),
	})

	// Check if cluster exists
	exists, err := provisioner.Exists(cmd.Context(), clusterInfo.ClusterName)
	if err != nil {
		return fmt.Errorf("check cluster existence: %w", err)
	}

	if !exists {
		return clustererrors.ErrClusterNotFound
	}

	// Disconnect registries before Talos cluster deletion to avoid network conflicts
	if clusterInfo.Distribution == v1alpha1.DistributionTalos {
		disconnectRegistriesBeforeTalosDelete(cmd, clusterInfo)
	}

	// Delete the cluster
	err = provisioner.Delete(cmd.Context(), clusterInfo.ClusterName)
	if err != nil {
		return fmt.Errorf("cluster deletion failed: %w", err)
	}

	outputTimer := helpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// disconnectRegistriesBeforeTalosDelete disconnects registries from the Talos network
// before cluster deletion. This is necessary because Talos deletes the network as part
// of cluster deletion, and connected containers prevent network removal.
func disconnectRegistriesBeforeTalosDelete(
	cmd *cobra.Command,
	clusterInfo *lifecycle.ClusterInfo,
) {
	cleanupDeps := getCleanupDeps()

	err := mirrorregistry.DisconnectRegistriesFromNetwork(
		cmd,
		clusterInfo.ClusterName, // Talos uses cluster name as network name
		cleanupDeps,
	)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to disconnect registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// cleanupRegistriesAfterDelete cleans up registries after cluster deletion.
func cleanupRegistriesAfterDelete(
	cmd *cobra.Command,
	tmr timer.Timer,
	clusterInfo *lifecycle.ClusterInfo,
	deleteStorage bool,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
) {
	cleanupDeps := getCleanupDeps()

	var err error
	if preDiscovered != nil && len(preDiscovered.Registries) > 0 {
		// Use pre-discovered registries (for Talos where network is destroyed)
		err = mirrorregistry.CleanupPreDiscoveredRegistries(
			cmd,
			tmr,
			preDiscovered.Registries,
			deleteStorage,
			cleanupDeps,
		)
	} else {
		// Discover and cleanup registries by network (for Kind, K3d)
		err = mirrorregistry.CleanupRegistriesByNetwork(
			cmd,
			tmr,
			clusterInfo.Distribution,
			clusterInfo.ClusterName,
			deleteStorage,
			cleanupDeps,
		)
	}

	if err != nil && !errors.Is(err, mirrorregistry.ErrNoRegistriesFound) {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to cleanup registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}
