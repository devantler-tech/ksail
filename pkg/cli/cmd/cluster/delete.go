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

const deleteLongDesc = `Destroy a cluster.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

The kubeconfig is resolved in the following priority order:
  1. From --kubeconfig flag
  2. From KUBECONFIG environment variable
  3. From ksail.yaml config file (if present)
  4. Defaults to ~/.kube/config`

// NewDeleteCmd creates and returns the delete command.
// Delete uses --name and --provider flags to determine the cluster to delete.
func NewDeleteCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	var (
		nameFlag       string
		providerFlag   v1alpha1.Provider
		kubeconfigFlag string
		deleteStorage  bool
	)

	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Destroy a cluster",
		Long:          deleteLongDesc,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeleteAction(
				cmd,
				runtimeContainer,
				nameFlag,
				providerFlag,
				kubeconfigFlag,
				deleteStorage,
			)
		},
	}

	cmd.Flags().StringVarP(
		&nameFlag,
		"name",
		"n",
		"",
		"Name of the cluster to delete",
	)

	cmd.Flags().VarP(
		&providerFlag,
		"provider",
		"p",
		fmt.Sprintf("Provider to use (%s)", providerFlag.ValidValues()),
	)

	cmd.Flags().StringVarP(
		&kubeconfigFlag,
		"kubeconfig",
		"k",
		"",
		"Path to kubeconfig file for context cleanup",
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
	nameFlag string,
	providerFlag v1alpha1.Provider,
	kubeconfigFlag string,
	deleteStorage bool,
) error {
	// Wrap output with StageSeparatingWriter for automatic stage separation
	stageWriter := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
	cmd.SetOut(stageWriter)

	// Get timer from runtime container using Invoke
	var tmr timer.Timer

	if runtimeContainer != nil {
		//nolint:wrapcheck // Error is captured to outer scope, not returned
		_ = runtimeContainer.Invoke(func(injector runtime.Injector) error {
			var err error

			tmr, err = runtime.ResolveTimer(injector)

			return err
		})
	}

	if tmr != nil {
		tmr.Start()
	}

	// Resolve cluster info from flags, config, or kubeconfig
	resolved, err := lifecycle.ResolveClusterInfo(nameFlag, providerFlag, kubeconfigFlag)
	if err != nil {
		return fmt.Errorf("failed to resolve cluster info: %w", err)
	}

	// Create cluster info for provisioner creation
	clusterInfo := &lifecycle.ClusterInfo{
		ClusterName:    resolved.ClusterName,
		Provider:       resolved.Provider,
		KubeconfigPath: resolved.KubeconfigPath,
	}

	// Create provisioner for the provider
	provisioner, err := createDeleteProvisioner(clusterInfo)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Pre-discover registries before deletion for Docker provider
	var preDiscovered *mirrorregistry.DiscoveredRegistries
	if resolved.Provider == v1alpha1.ProviderDocker {
		preDiscovered = discoverRegistriesBeforeDelete(cmd, clusterInfo)
	}

	// Delete the cluster
	err = executeDelete(cmd, tmr, provisioner, resolved)
	if err != nil {
		return err
	}

	// Cleanup registries after cluster deletion (only for Docker provider)
	if resolved.Provider == v1alpha1.ProviderDocker {
		cleanupRegistriesAfterDelete(cmd, tmr, resolved, deleteStorage, preDiscovered)
	}

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

	provisioner, err := lifecycle.CreateMinimalProvisionerForProvider(clusterInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create provisioner for provider: %w", err)
	}

	return provisioner, nil
}

// discoverRegistriesBeforeDelete discovers registries connected to the cluster network.
// This must be called BEFORE cluster deletion for Docker-based clusters.
func discoverRegistriesBeforeDelete(
	cmd *cobra.Command,
	clusterInfo *lifecycle.ClusterInfo,
) *mirrorregistry.DiscoveredRegistries {
	cleanupDeps := getCleanupDeps()

	// For Docker provider, we need to try all distributions
	// Use Talos as the distribution hint since registry cleanup uses cluster name as network name
	return mirrorregistry.DiscoverRegistriesByNetwork(
		cmd,
		v1alpha1.DistributionTalos, // Distribution hint for network naming
		clusterInfo.ClusterName,
		cleanupDeps,
	)
}

// executeDelete performs the cluster deletion operation.
func executeDelete(
	cmd *cobra.Command,
	tmr timer.Timer,
	provisioner clusterprovisioner.ClusterProvisioner,
	resolved *lifecycle.ResolvedClusterInfo,
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
			"deleting cluster '%s' on %s",
			resolved.ClusterName,
			resolved.Provider,
		),
		Writer: cmd.OutOrStdout(),
	})

	// Check if cluster exists
	exists, err := provisioner.Exists(cmd.Context(), resolved.ClusterName)
	if err != nil {
		return fmt.Errorf("check cluster existence: %w", err)
	}

	if !exists {
		return clustererrors.ErrClusterNotFound
	}

	// Delete the cluster
	err = provisioner.Delete(cmd.Context(), resolved.ClusterName)
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

// cleanupRegistriesAfterDelete cleans up registries after cluster deletion.
func cleanupRegistriesAfterDelete(
	cmd *cobra.Command,
	tmr timer.Timer,
	resolved *lifecycle.ResolvedClusterInfo,
	deleteStorage bool,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
) {
	cleanupDeps := getCleanupDeps()

	var err error
	if preDiscovered != nil && len(preDiscovered.Registries) > 0 {
		// Use pre-discovered registries
		err = mirrorregistry.CleanupPreDiscoveredRegistries(
			cmd,
			tmr,
			preDiscovered.Registries,
			deleteStorage,
			cleanupDeps,
		)
	} else {
		// Discover and cleanup registries by network
		// Use Talos as distribution hint since registry cleanup uses cluster name as network name
		err = mirrorregistry.CleanupRegistriesByNetwork(
			cmd,
			tmr,
			v1alpha1.DistributionTalos,
			resolved.ClusterName,
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
