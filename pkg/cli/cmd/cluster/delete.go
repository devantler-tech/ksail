package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
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

// deleteFlags holds all the flags for the delete command.
type deleteFlags struct {
	name       string
	provider   v1alpha1.Provider
	kubeconfig string
	storage    bool
	force      bool
}

// NewDeleteCmd creates and returns the delete command.
// Delete uses --name and --provider flags to determine the cluster to delete.
func NewDeleteCmd(runtimeContainer *di.Runtime) *cobra.Command {
	flags := &deleteFlags{}

	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Destroy a cluster",
		Long:          deleteLongDesc,
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeleteAction(cmd, runtimeContainer, flags)
		},
	}

	registerDeleteFlags(cmd, flags)

	return cmd
}

// registerDeleteFlags registers all flags for the delete command.
func registerDeleteFlags(cmd *cobra.Command, flags *deleteFlags) {
	cmd.Flags().StringVarP(&flags.name, "name", "n", "", "Name of the cluster to delete")
	cmd.Flags().VarP(&flags.provider, "provider", "p",
		fmt.Sprintf("Provider to use (%s)", flags.provider.ValidValues()))
	cmd.Flags().StringVarP(&flags.kubeconfig, "kubeconfig", "k", "",
		"Path to kubeconfig file for context cleanup")
	cmd.Flags().BoolVar(&flags.storage, "delete-storage", false,
		"Delete storage volumes when cleaning up (registry volumes for Docker, block storage for Hetzner)")
	cmd.Flags().BoolVarP(&flags.force, "force", "f", false,
		"Skip confirmation prompt and delete immediately")
}

// runDeleteAction executes the cluster deletion with registry cleanup.
func runDeleteAction(
	cmd *cobra.Command,
	runtimeContainer *di.Runtime,
	flags *deleteFlags,
) error {
	// Wrap output with StageSeparatingWriter for automatic stage separation
	stageWriter := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
	cmd.SetOut(stageWriter)

	tmr := initTimer(runtimeContainer)

	// Resolve cluster info from flags, config, or kubeconfig
	resolved, err := lifecycle.ResolveClusterInfo(flags.name, flags.provider, flags.kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to resolve cluster info: %w", err)
	}

	// Detect cluster distribution and info before deletion
	// This must happen before deletion while kubeconfig is still available
	detectedInfo := detectClusterDistribution(resolved)
	isKindCluster := detectedInfo != nil &&
		detectedInfo.Distribution == v1alpha1.DistributionVanilla

	// Fallback: detect Kind cluster from container naming patterns if kubeconfig detection failed
	// This handles cases where kubeconfig context is missing but cluster containers exist
	if !isKindCluster && resolved.Provider == v1alpha1.ProviderDocker {
		nodes := discoverDockerNodes(cmd, resolved.ClusterName)
		isKindCluster = isKindClusterFromNodes(nodes, resolved.ClusterName)
	}

	// Create cluster info for provisioner creation, including detected distribution
	clusterInfo := &clusterdetector.Info{
		ClusterName:    resolved.ClusterName,
		Provider:       resolved.Provider,
		KubeconfigPath: resolved.KubeconfigPath,
	}
	if detectedInfo != nil {
		clusterInfo.Distribution = detectedInfo.Distribution
	}

	// Create provisioner for the provider
	provisioner, err := createDeleteProvisioner(clusterInfo)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Pre-discover registries before deletion for Docker provider
	preDiscovered := prepareDockerDeletion(cmd, resolved, clusterInfo)

	// Show confirmation prompt unless force flag is set or non-TTY
	if !confirm.ShouldSkipPrompt(flags.force) {
		err := promptForDeletion(cmd, resolved, preDiscovered, isKindCluster)
		if err != nil {
			return err
		}
	}

	// Delete the cluster
	err = executeDelete(cmd, tmr, provisioner, resolved)
	if err != nil {
		return err
	}

	// Perform post-deletion cleanup
	performPostDeletionCleanup(cmd, tmr, resolved, flags, preDiscovered, isKindCluster)

	return nil
}

// detectClusterDistribution detects the distribution and other cluster info.
// This detection must happen before the cluster is deleted to ensure the kubeconfig
// entry is still available for reading cluster information.
// Returns nil if detection fails or the provider is not Docker.
func detectClusterDistribution(resolved *lifecycle.ResolvedClusterInfo) *clusterdetector.Info {
	if resolved.Provider != v1alpha1.ProviderDocker {
		return nil
	}

	name := strings.TrimSpace(resolved.ClusterName)

	// Each distribution uses a different kubeconfig context naming convention.
	prefixes := []string{
		"kind-",
		"k3d-",
		"vcluster-docker_",
	}

	for _, prefix := range prefixes {
		contextName := ""

		if name != "" {
			contextName = prefix + name
		}

		info, err := clusterdetector.DetectInfo(resolved.KubeconfigPath, contextName)
		if err == nil && info != nil {
			return info
		}
	}

	return nil
}

// prepareDockerDeletion prepares Docker-specific resources before deletion.
func prepareDockerDeletion(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	clusterInfo *clusterdetector.Info,
) *mirrorregistry.DiscoveredRegistries {
	if resolved.Provider != v1alpha1.ProviderDocker {
		return nil
	}

	preDiscovered := discoverRegistriesBeforeDelete(cmd, clusterInfo)
	disconnectRegistriesBeforeDelete(cmd, resolved.ClusterName)

	return preDiscovered
}

// performPostDeletionCleanup handles all post-deletion cleanup tasks.
func performPostDeletionCleanup(
	cmd *cobra.Command,
	tmr timer.Timer,
	resolved *lifecycle.ResolvedClusterInfo,
	flags *deleteFlags,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) {
	// Cleanup registries after cluster deletion (only for Docker provider)
	if resolved.Provider == v1alpha1.ProviderDocker {
		cleanupRegistriesAfterDelete(cmd, tmr, resolved, flags.storage, preDiscovered)
	}

	// Cleanup cloud-provider-kind if this was the last kind cluster
	// Only run for Vanilla (Kind) distribution on Docker provider
	if isKindCluster {
		cleanupCloudProviderKindIfLastCluster(cmd, tmr)
	}
}

// initTimer initializes and starts the timer from the runtime container.
func initTimer(runtimeContainer *di.Runtime) timer.Timer {
	var tmr timer.Timer

	if runtimeContainer != nil {
		//nolint:wrapcheck // Error is captured to outer scope, not returned
		_ = runtimeContainer.Invoke(func(injector di.Injector) error {
			var err error

			tmr, err = di.ResolveTimer(injector)

			return err
		})
	}

	if tmr != nil {
		tmr.Start()
	}

	return tmr
}

// promptForDeletion shows the deletion preview and prompts for confirmation.
func promptForDeletion(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) error {
	preview := buildDeletionPreview(cmd, resolved, preDiscovered, isKindCluster)
	confirm.ShowDeletionPreview(cmd.OutOrStdout(), preview)

	if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
		return confirm.ErrDeletionCancelled
	}

	return nil
}

// createDeleteProvisioner creates the appropriate provisioner for cluster deletion.
// It first checks for test overrides, then falls back to creating a minimal provisioner.
func createDeleteProvisioner(
	clusterInfo *clusterdetector.Info,
) (clusterprovisioner.Provisioner, error) {
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
	clusterInfo *clusterdetector.Info,
) *mirrorregistry.DiscoveredRegistries {
	cleanupDeps := getCleanupDeps()

	// Use the detected distribution for correct network name resolution
	// Kind uses fixed "kind" network, Talos uses cluster name as network name
	distribution := clusterInfo.Distribution
	if distribution == "" {
		// Fallback to Talos if distribution is unknown (uses cluster name as network)
		distribution = v1alpha1.DistributionTalos
	}

	return mirrorregistry.DiscoverRegistriesByNetwork(
		cmd,
		distribution,
		clusterInfo.ClusterName,
		cleanupDeps,
	)
}

// disconnectRegistriesBeforeDelete disconnects registries from the cluster network.
// This is required for Talos because it destroys the network during deletion,
// and the deletion will fail if containers are still connected to the network.
func disconnectRegistriesBeforeDelete(cmd *cobra.Command, clusterName string) {
	cleanupDeps := getCleanupDeps()

	// For Talos, the network name is the cluster name
	// We silently disconnect registries - errors are ignored since the cluster
	// may not have any registries connected, or the network may not exist
	_ = mirrorregistry.DisconnectRegistriesFromNetwork(cmd, clusterName, cleanupDeps)
}

// buildDeletionPreview builds a preview of resources that will be deleted.
func buildDeletionPreview(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) *confirm.DeletionPreview {
	preview := &confirm.DeletionPreview{
		ClusterName: resolved.ClusterName,
		Provider:    resolved.Provider,
	}

	switch resolved.Provider {
	case v1alpha1.ProviderDocker:
		// Collect registry names
		if preDiscovered != nil {
			for _, reg := range preDiscovered.Registries {
				preview.Registries = append(preview.Registries, reg.Name)
			}
		}

		// Try to discover cluster node containers
		preview.Nodes = discoverDockerNodes(cmd, resolved.ClusterName)

		// If this is the last Kind cluster, show shared containers that will be deleted
		if isKindCluster && countKindClusters(cmd) == 1 {
			preview.SharedContainers = listCloudProviderKindContainerNames(cmd)
		}
	case v1alpha1.ProviderHetzner:
		// For Hetzner, resources follow predictable naming patterns
		// Note: We can't list actual servers without API access, but we know infrastructure resources
		preview.PlacementGroup = resolved.ClusterName + "-placement"
		preview.Firewall = resolved.ClusterName + "-firewall"
		preview.Network = resolved.ClusterName + "-network"
		// Servers are labeled but we don't have API access here to list them
		// Add a placeholder to indicate servers will be deleted
		serverPlaceholder := "(all servers labeled with cluster: " + resolved.ClusterName + ")"
		preview.Servers = []string{serverPlaceholder}
	}

	return preview
}

// executeDelete performs the cluster deletion operation.
func executeDelete(
	cmd *cobra.Command,
	tmr timer.Timer,
	provisioner clusterprovisioner.Provisioner,
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
		return clustererr.ErrClusterNotFound
	}

	// Delete the cluster
	err = provisioner.Delete(cmd.Context(), resolved.ClusterName)
	if err != nil {
		return fmt.Errorf("cluster deletion failed: %w", err)
	}

	outputTimer := flags.MaybeTimer(cmd, tmr)

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
		// Use Talos as fallback since it uses cluster name as network name
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
