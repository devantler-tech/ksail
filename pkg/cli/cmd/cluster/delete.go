package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
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
func NewDeleteCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
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
	runtimeContainer *runtime.Runtime,
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

	// Detect if this is a Kind cluster for cleanup decisions
	isKindCluster := detectIsKindCluster(resolved)

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
	preDiscovered := prepareDockerDeletion(cmd, resolved, clusterInfo)

	// Show confirmation prompt unless force flag is set or non-TTY
	if !confirm.ShouldSkipPrompt(flags.force) {
		err := promptForDeletion(cmd, resolved, preDiscovered)
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

// detectIsKindCluster determines if the cluster is a Kind (Vanilla) cluster.
// This detection must happen before the cluster is deleted to ensure the kubeconfig
// entry is still available for reading cluster information.
func detectIsKindCluster(resolved *lifecycle.ResolvedClusterInfo) bool {
	if resolved.Provider != v1alpha1.ProviderDocker {
		return false
	}

	contextName := ""
	if strings.TrimSpace(resolved.ClusterName) != "" {
		contextName = "kind-" + resolved.ClusterName
	}

	clusterInfo, detectErr := lifecycle.DetectClusterInfo(resolved.KubeconfigPath, contextName)
	if detectErr != nil || clusterInfo == nil {
		return false
	}

	return clusterInfo.Distribution == v1alpha1.DistributionVanilla
}

// prepareDockerDeletion prepares Docker-specific resources before deletion.
func prepareDockerDeletion(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	clusterInfo *lifecycle.ClusterInfo,
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
func initTimer(runtimeContainer *runtime.Runtime) timer.Timer {
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

	return tmr
}

// promptForDeletion shows the deletion preview and prompts for confirmation.
func promptForDeletion(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
) error {
	preview := buildDeletionPreview(cmd, resolved, preDiscovered)
	confirm.ShowDeletionPreview(cmd.OutOrStdout(), preview)

	if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
		return confirm.ErrDeletionCancelled
	}

	return nil
}

// createDeleteProvisioner creates the appropriate provisioner for cluster deletion.
// It first checks for test overrides, then falls back to creating a minimal provisioner.
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

// discoverDockerNodes discovers cluster node containers for Docker provider.
func discoverDockerNodes(cmd *cobra.Command, clusterName string) []string {
	var nodes []string

	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	// Try to list containers matching cluster name patterns
	// Kind uses: {cluster}-control-plane, {cluster}-worker, etc.
	// K3d uses: k3d-{cluster}-server-0, k3d-{cluster}-agent-0, etc.
	// Talos uses: {cluster}-controlplane-*, {cluster}-worker-*
	_ = invoker(cmd, func(dockerClient client.APIClient) error {
		containers, err := dockerClient.ContainerList(cmd.Context(), container.ListOptions{
			All: true,
		})
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		for _, ctr := range containers {
			for _, name := range ctr.Names {
				// Remove leading slash from container name
				containerName := strings.TrimPrefix(name, "/")

				// Check if container belongs to this cluster
				if IsClusterContainer(containerName, clusterName) {
					nodes = append(nodes, containerName)
				}
			}
		}

		return nil
	})

	return nodes
}

// IsClusterContainer checks if a container name belongs to the given cluster.
// Exported for testing.
func IsClusterContainer(containerName, clusterName string) bool {
	// Kind pattern: {cluster}-control-plane, {cluster}-worker, {cluster}-worker{N}
	// Check for exact prefixes with valid suffixes to avoid partial cluster name matches
	if matchesKindPattern(containerName, clusterName) {
		return true
	}

	// K3d pattern: k3d-{cluster}-server-*, k3d-{cluster}-agent-*
	if strings.HasPrefix(containerName, "k3d-"+clusterName+"-server-") ||
		strings.HasPrefix(containerName, "k3d-"+clusterName+"-agent-") {
		return true
	}

	// Talos pattern: {cluster}-controlplane-*, {cluster}-worker-*
	if strings.HasPrefix(containerName, clusterName+"-controlplane-") ||
		strings.HasPrefix(containerName, clusterName+"-worker-") {
		return true
	}

	return false
}

// matchesKindPattern checks if container matches Kind's naming convention.
// Kind uses: {cluster}-control-plane, {cluster}-worker, {cluster}-worker{N}.
func matchesKindPattern(containerName, clusterName string) bool {
	// Check control-plane (exact suffix)
	if containerName == clusterName+"-control-plane" {
		return true
	}

	// Check worker nodes: {cluster}-worker or {cluster}-worker{N}
	workerPrefix := clusterName + "-worker"
	if containerName == workerPrefix {
		return true
	}

	// Check for numbered workers: {cluster}-worker2, {cluster}-worker3, etc.
	if strings.HasPrefix(containerName, workerPrefix) {
		suffix := containerName[len(workerPrefix):]
		// Suffix must be a number for valid worker nodes
		if suffix != "" && isNumericString(suffix) {
			return true
		}
	}

	return false
}

// isNumericString checks if a string contains only digits.
func isNumericString(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
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

// hasRemainingKindClusters checks if there are any Kind clusters remaining in Docker.
func hasRemainingKindClusters(cmd *cobra.Command) bool {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	var hasKind bool

	_ = invoker(nil, func(dockerClient client.APIClient) error {
		ctx := cmd.Context()

		containers, err := dockerClient.ContainerList(ctx, container.ListOptions{
			All: true,
		})
		if err != nil {
			return fmt.Errorf("failed to list Docker containers: %w", err)
		}

		// Check if any container matches Kind's naming pattern
		// Kind containers end with -control-plane or -worker*
		for _, ctr := range containers {
			for _, name := range ctr.Names {
				containerName := strings.TrimPrefix(name, "/")
				// Kind control plane pattern: *-control-plane
				if strings.HasSuffix(containerName, "-control-plane") {
					hasKind = true

					return nil
				}
			}
		}

		return nil
	})

	return hasKind
}

// hasCloudProviderKindContainers checks if there are any cloud-provider-kind containers.
// This includes both the main ksail-cloud-provider-kind controller and cpk-* service containers.
func hasCloudProviderKindContainers(cmd *cobra.Command) bool {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	var hasContainers bool

	_ = invoker(nil, func(dockerClient client.APIClient) error {
		containers, err := dockerClient.ContainerList(cmd.Context(), container.ListOptions{
			All: true,
		})
		if err != nil {
			return fmt.Errorf("failed to list Docker containers: %w", err)
		}

		// Check if any container matches cloud-provider-kind's naming pattern
		for _, ctr := range containers {
			for _, name := range ctr.Names {
				containerName := strings.TrimPrefix(name, "/")
				// Check for main controller container or cpk-* service containers
				if containerName == "ksail-cloud-provider-kind" ||
					strings.HasPrefix(containerName, "cpk-") {
					hasContainers = true

					return nil
				}
			}
		}

		return nil
	})

	return hasContainers
}

// cleanupCloudProviderKindIfLastCluster uninstalls cloud-provider-kind if no kind clusters remain.
// Cloud-provider-kind creates containers that can be shared across multiple kind clusters,
// so we only uninstall when the last kind cluster is deleted.
func cleanupCloudProviderKindIfLastCluster(
	cmd *cobra.Command,
	tmr timer.Timer,
) {
	// Check if there are any remaining Kind clusters by looking for Kind containers
	if hasRemainingKindClusters(cmd) {
		return
	}

	// Check if there are any cloud-provider-kind containers to clean up
	if !hasCloudProviderKindContainers(cmd) {
		return
	}

	// No kind clusters remain - proceed with cloud-provider-kind cleanup
	if tmr != nil {
		tmr.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Cleanup cloud-provider-kind...",
		Emoji:   "🧹",
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "uninstalling cloud-provider-kind (no kind clusters remain)",
		Writer:  cmd.OutOrStdout(),
	})

	// We need to uninstall from one of the recently deleted clusters
	// Since all clusters are gone, we can't actually uninstall via Helm
	// Instead, we need to clean up any remaining cloud-provider-kind containers
	cleanupErr := cleanupCloudProviderKindContainers(cmd)
	if cleanupErr != nil {
		notify.WriteMessage(notify.Message{
			Type: notify.WarningType,
			Content: fmt.Sprintf(
				"failed to cleanup cloud-provider-kind containers: %v",
				cleanupErr,
			),
			Writer: cmd.OutOrStdout(),
		})

		return
	}

	outputTimer := helpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cloud-provider-kind cleaned up",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})
}

// cleanupCloudProviderKindContainers removes any cloud-provider-kind related containers.
// This includes:
// - The main ksail-cloud-provider-kind controller container
// - Any cpk-* containers created by cloud-provider-kind for LoadBalancer services.
func cleanupCloudProviderKindContainers(cmd *cobra.Command) error {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	var cleanupErr error

	_ = invoker(cmd, func(dockerClient client.APIClient) error {
		containers, err := dockerClient.ContainerList(cmd.Context(), container.ListOptions{
			All: true,
		})
		if err != nil {
			cleanupErr = fmt.Errorf("failed to list containers: %w", err)

			return cleanupErr
		}

		for _, ctr := range containers {
			// Cloud-provider-kind creates containers for load balancer services
			// These containers are named with a specific pattern
			// Look for containers created by cloud-provider-kind
			for _, name := range ctr.Names {
				containerName := strings.TrimPrefix(name, "/")
				// Remove the main ksail-cloud-provider-kind controller container
				// and cpk-* containers (named: cpk-<service>-<namespace>-<cluster>)
				if containerName == "ksail-cloud-provider-kind" ||
					strings.HasPrefix(containerName, "cpk-") {
					err := dockerClient.ContainerRemove(
						cmd.Context(),
						ctr.ID,
						container.RemoveOptions{
							Force: true,
						},
					)
					if err != nil {
						cleanupErr = fmt.Errorf(
							"failed to remove container %s: %w",
							containerName,
							err,
						)

						return cleanupErr
					}
				}
			}
		}

		return nil
	})

	return cleanupErr
}
