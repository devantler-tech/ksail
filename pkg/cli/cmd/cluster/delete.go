package cluster

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/docker/docker/api/types/container"
	"github.com/spf13/cobra"
)

// deleteFlags holds all the flags for the delete command. The shared
// cluster-targeting flags (--name/--provider/--kubeconfig) are embedded via
// lifecycle.ClusterTargetFlags so delete registers them identically to the rest
// of the cluster group.
type deleteFlags struct {
	lifecycle.ClusterTargetFlags

	storage bool
	force   bool
}

// NewDeleteCmd creates and returns the delete command.
// Delete uses --name and --provider flags to determine the cluster to delete.
func NewDeleteCmd() *cobra.Command {
	flags := &deleteFlags{}

	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Destroy a cluster",
		Long:          lifecycle.WithClusterTargetingHelp("Destroy a cluster."),
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeleteAction(cmd, flags)
		},
	}

	registerDeleteFlags(cmd, flags)

	return cmd
}

// registerDeleteFlags registers all flags for the delete command.
func registerDeleteFlags(cmd *cobra.Command, flags *deleteFlags) {
	lifecycle.RegisterClusterTargetFlags(
		cmd, &flags.ClusterTargetFlags,
		"Name of the cluster to delete",
		"Path to kubeconfig file for context cleanup",
	)
	cmd.Flags().BoolVar(&flags.storage, "delete-storage", false,
		"Delete storage volumes when cleaning up (registry volumes for Docker, block storage for Hetzner)")
	cmd.Flags().BoolVarP(&flags.force, "force", "f", false,
		"Skip confirmation prompt and delete immediately")
	_ = cmd.Flags().SetAnnotation(
		forceFlagName, annotations.AnnotationConfirmFlag,
		[]string{annotations.AnnotationValueTrue},
	)
}

// deleteUnmanagedGuardFunc is the unmanaged-cluster guard the delete command applies before it
// touches the target. It defaults to the real cross-provider guard; tests override it (via
// ExportSetDeleteUnmanagedGuard) so the refusal path can be exercised without a live provider.
//
//nolint:gochecknoglobals // dependency injection for tests
var deleteUnmanagedGuardFunc = unmanagedClusterGuard

// runDeleteAction executes the cluster deletion with registry cleanup.
func runDeleteAction(
	cmd *cobra.Command,
	flags *deleteFlags,
) error {
	// Wrap output with StageSeparatingWriter for automatic stage separation
	stageWriter := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
	cmd.SetOut(stageWriter)

	tmr := timer.New()
	tmr.Start()

	// Strict: delete is destructive, so an unreadable config aborts before anything is removed.
	resolved, err := lifecycle.ResolveClusterInfoStrict(
		cmd, flags.Name, flags.Provider, flags.Kubeconfig,
	)
	if err != nil {
		return fmt.Errorf("failed to resolve cluster info: %w", err)
	}

	err = lifecycle.ValidateStandaloneAWSTarget(resolved)
	if err != nil {
		return fmt.Errorf("validate standalone AWS target: %w", err)
	}

	// Refuse to destroy a cluster ksail did not provision. When the resolved context is an unmanaged
	// cluster (a managed cloud cluster, a kubeadm cluster, a colleague's cluster) the guard rejects
	// here — before any provisioner is created or the cluster is touched — so ksail never accidentally
	// deletes a cluster it does not own. Read-only operations still work. (ksail#5885, epic #5654.)
	guardErr := deleteUnmanagedGuardFunc(cmd.Context(), resolved)
	if guardErr != nil {
		return handleUnmanagedDeleteTarget(cmd, tmr, resolved, flags, guardErr)
	}

	// Detect cluster distribution and info before deletion
	// This must happen before deletion while kubeconfig is still available
	detectedInfo, isKindCluster, clusterInfo := detectDeleteClusterInfo(cmd, resolved)

	// Create provisioner for the provider
	options := minimalProvisionerOptions(resolved, flags.storage)

	provisioner, err := createDeleteProvisioner(cmd.Context(), clusterInfo, options)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Pre-discover registries before deletion for Docker provider
	preDiscovered := prepareDockerDeletion(cmd, resolved, clusterInfo)

	err = confirmAndReverifyDeleteTarget(cmd, flags, resolved, preDiscovered, isKindCluster)
	if err != nil {
		return err
	}

	err = executeDeleteTolerantOfMissingNodes(cmd, tmr, provisioner, resolved)
	if err != nil {
		return err
	}

	// Perform post-deletion cleanup
	performPostDeletionCleanup(
		cmd,
		tmr,
		resolved,
		flags,
		preDiscovered,
		isKindCluster,
		detectedInfo,
	)

	return nil
}

func confirmAndReverifyDeleteTarget(
	cmd *cobra.Command,
	flags *deleteFlags,
	resolved *lifecycle.ResolvedClusterInfo,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) error {
	// Show confirmation prompt unless force flag is set or non-TTY.
	if !confirm.ShouldSkipPrompt(flags.force) {
		err := promptForDeletion(cmd, resolved, preDiscovered, isKindCluster)
		if err != nil {
			return err
		}
	}

	err := lifecycle.VerifyAWSOwnershipBeforeMutation(
		cmd.Context(),
		resolved.AWSOwnershipVerifier,
	)
	if err != nil {
		return fmt.Errorf("reverify EKS ownership after delete confirmation: %w", err)
	}

	return nil
}

func minimalProvisionerOptions(
	resolved *lifecycle.ResolvedClusterInfo,
	deleteStorage bool,
) lifecycle.MinimalProvisionerOptions {
	return lifecycle.MinimalProvisionerOptions{
		OmniOpts:             resolved.OmniOpts,
		KubernetesOpts:       resolved.KubernetesOpts,
		AWSOpts:              resolved.AWSOpts,
		AWSRegion:            resolved.AWSRegion,
		AWSResolution:        resolved.AWSResolution,
		AWSOwnershipVerifier: resolved.AWSOwnershipVerifier,
		DeleteStorage:        deleteStorage,
	}
}

// detectClusterDistribution detects the distribution and other cluster info.
// This detection must happen before the cluster is deleted to ensure the kubeconfig
// entry is still available for reading cluster information.
// Returns nil if detection fails or the provider is not Docker.
func detectClusterDistribution(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
) *clusterdetector.Info {
	if resolved.Provider != v1alpha1.ProviderDocker {
		return nil
	}

	name := strings.TrimSpace(resolved.ClusterName)

	// Each distribution uses a different kubeconfig context naming convention.
	// ContextPrefixes is the single source of these conventions and also covers
	// Talos ("admin@") and the nested-on-Kubernetes K3s ("k3k-") alias, so
	// delete now detects clusters those prefixes name (release-note tagged).
	for _, prefix := range clusterdetector.ContextPrefixes() {
		contextName := ""

		if name != "" {
			contextName = prefix + name
		}

		info, err := clusterdetector.DetectInfo(ctx, resolved.KubeconfigPath, contextName)
		if err == nil && info != nil {
			return info
		}
	}

	return nil
}

// detectDeleteClusterInfo detects the distribution, Kind-cluster status, and builds
// the clusterdetector.Info needed for provisioner creation during cluster deletion.
func detectDeleteClusterInfo(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
) (*clusterdetector.Info, bool, *clusterdetector.Info) {
	detectedInfo := detectClusterDistribution(cmd.Context(), resolved)
	isKindCluster := detectedInfo != nil &&
		detectedInfo.Distribution == v1alpha1.DistributionVanilla

	// Fallback: detect Kind cluster from container naming patterns if kubeconfig detection failed
	if !isKindCluster && resolved.Provider == v1alpha1.ProviderDocker {
		nodes := discoverDockerNodes(cmd, resolved.ClusterName)
		isKindCluster = isKindClusterFromNodes(nodes, resolved.ClusterName)
	}

	clusterInfo := &clusterdetector.Info{
		ClusterName:    resolved.ClusterName,
		Provider:       resolved.Provider,
		KubeconfigPath: resolved.KubeconfigPath,
	}
	if detectedInfo != nil {
		clusterInfo.Distribution = detectedInfo.Distribution
	}

	// Carry the container-name fallback into the distribution. Without this the fallback is
	// computed and then discarded, so a Kind cluster whose kubeconfig detection failed resolves
	// its registry network as Talos (the cluster name) instead of "kind" — discovery finds
	// nothing and the registries leak silently (#6286).
	if clusterInfo.Distribution == "" && isKindCluster {
		clusterInfo.Distribution = v1alpha1.DistributionVanilla
	}

	return detectedInfo, isKindCluster, clusterInfo
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
	disconnectRegistriesBeforeDelete(cmd, clusterInfo, preDiscovered)

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
	detectedInfo *clusterdetector.Info,
) {
	// Cleanup registries after cluster deletion (only for Docker provider)
	if resolved.Provider == v1alpha1.ProviderDocker {
		cleanupRegistriesAfterDelete(cmd, tmr, resolved, flags.storage, preDiscovered)
	}

	// Cleanup OIDC kubeconfig entries (user + context) if they exist.
	// Derive the display name using the same ExtractClusterNameFromContext logic
	// as the create path (configureOIDCKubeconfig) to ensure consistent naming.
	oidcDisplayName := resolved.ClusterName

	if detectedInfo != nil && detectedInfo.Context != "" {
		if name := lifecycle.ExtractClusterNameFromContext(
			detectedInfo.Context,
			detectedInfo.Distribution,
		); name != "" {
			oidcDisplayName = name
		}
	}

	// This is a best-effort cleanup — errors are logged as warnings.
	cleanupErr := k8s.CleanupOIDCKubeconfigEntries(
		resolved.KubeconfigPath,
		oidcDisplayName,
		cmd.OutOrStdout(),
	)
	if cleanupErr != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "failed to clean up OIDC kubeconfig entries: %v",
			Args:    []any{cleanupErr},
			Writer:  cmd.OutOrStderr(),
		})
	}

	// Cleanup cloud-provider-kind if this was the last kind cluster
	// Only run for Vanilla (Kind) distribution on Docker provider
	if isKindCluster {
		cleanupCloudProviderKindIfLastCluster(cmd, tmr)
	}
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
	ctx context.Context,
	clusterInfo *clusterdetector.Info,
	options lifecycle.MinimalProvisionerOptions,
) (clusterprovisioner.Provisioner, error) {
	// Check for test factory override
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		provisioner, _, err := factoryOverride.Create(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("factory override failed: %w", err)
		}

		return provisioner, nil
	}

	provisioner, err := lifecycle.CreateMinimalProvisionerForProvider(
		ctx, clusterInfo, options,
	)
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
// This is required for distributions like Talos and VCluster because they destroy
// the network during deletion, and the deletion will fail if containers are still
// connected to the network.
func disconnectRegistriesBeforeDelete(
	cmd *cobra.Command,
	clusterInfo *clusterdetector.Info,
	_ *mirrorregistry.DiscoveredRegistries,
) {
	cleanupDeps := getCleanupDeps()

	// Resolve the distribution-specific network name
	distribution := clusterInfo.Distribution
	if distribution == "" {
		distribution = v1alpha1.DistributionTalos
	}

	networkName := mirrorregistry.GetNetworkNameForDistribution(
		distribution,
		clusterInfo.ClusterName,
	)

	// Selected by NETWORK MEMBERSHIP, not by name: a local mirror endpoint configured without a
	// prefix keeps its bare name, so a name-scoped filter would skip it and leave the network
	// populated — which then blocks the distribution from deleting that network.
	//
	// Registries attributable to another cluster are still excluded, because the Kind network is
	// shared by every Kind cluster on the host and this runs before the confirmation prompt.
	//
	// Errors are ignored: a registry may already be gone or never connected.
	toDisconnect := mirrorregistry.DiscoverRegistriesToDisconnect(
		cmd,
		distribution,
		clusterInfo.ClusterName,
		cleanupDeps,
	)

	_ = mirrorregistry.DisconnectRegistriesByInfo(
		cmd,
		networkName,
		toDisconnect.Registries,
		cleanupDeps,
	)
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
		buildDockerDeletionPreview(cmd, resolved, preview, preDiscovered, isKindCluster)
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
	case v1alpha1.ProviderOmni:
		// For Omni, the cluster resource will be destroyed which deallocates all machines
		machinePlaceholder := "(all machines allocated to cluster: " + resolved.ClusterName + ")"
		preview.Servers = []string{machinePlaceholder}
	case v1alpha1.ProviderAWS:
		// For AWS/EKS, deletion is delegated to eksctl which tears down the
		// CloudFormation stacks owning the control plane and managed nodegroups.
		eksPlaceholder := "(EKS cluster and managed nodegroups for: " + resolved.ClusterName + ")"
		preview.Servers = []string{eksPlaceholder}
	case v1alpha1.ProviderGCP:
		// For GCP/GKE, deletion goes through the GKE API which tears down the
		// managed control plane and its node pools.
		gkePlaceholder := "(GKE cluster and node pools for: " + resolved.ClusterName + ")"
		preview.Servers = []string{gkePlaceholder}
	case v1alpha1.ProviderAzure:
		// For Azure/AKS, deletion goes through the AKS API which tears down the
		// managed control plane and its agent pools.
		aksPlaceholder := "(AKS cluster and agent pools for: " + resolved.ClusterName + ")"
		preview.Servers = []string{aksPlaceholder}
	case v1alpha1.ProviderKubernetes:
		// For Kubernetes provider, nested cluster resources (DinD pod, Gateway,
		// namespace) will be removed from the host cluster.
		k8sPlaceholder := "(nested cluster namespace and resources for: " + resolved.ClusterName + ")"
		preview.Servers = []string{k8sPlaceholder}
	}

	return preview
}

// buildDockerDeletionPreview fills the preview with the Docker-provider resources: discovered
// registries, cluster node containers, and — when the last Kind cluster is being deleted — the
// shared cloud-provider-kind containers that go with it.
func buildDockerDeletionPreview(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	preview *confirm.DeletionPreview,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) {
	if preDiscovered != nil {
		for _, reg := range preDiscovered.Registries {
			preview.Registries = append(preview.Registries, reg.Name)
		}
	}

	preview.Nodes = discoverDockerNodes(cmd, resolved.ClusterName)

	if isKindCluster && countKindClusters(cmd) == 1 {
		preview.SharedContainers = listCloudProviderKindContainerNames(cmd)
	}
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

	// Clean up persisted state (spec + TTL) for the deleted cluster.
	// Best-effort: log a warning on failure rather than blocking success.
	stateErr := state.DeleteClusterState(resolved.ClusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(), "failed to clean up cluster state: %v", stateErr)
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
		// Nothing was pre-discovered. The network may already be gone (or was resolved from a
		// misdetected distribution), so fall back to network-independent discovery by cluster
		// name rather than guessing a network name — guessing is what let registries leak (#6286).
		remnant := mirrorregistry.DiscoverClusterRegistryRemnant(
			cmd,
			resolved.ClusterName,
			cleanupDeps,
		)

		if len(remnant.Registries) == 0 {
			return
		}

		err = mirrorregistry.CleanupPreDiscoveredRegistries(
			cmd,
			tmr,
			remnant.Registries,
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

	reportRegistryLeftovers(cmd, resolved.ClusterName, cleanupDeps)
}

// handleUnmanagedDeleteTarget decides what to do when the unmanaged-cluster guard refuses.
//
// A surviving KSail-owned registry container proves KSail created THAT CONTAINER. It does not
// prove the kubeconfig target is KSail's cluster, and it does not prove the cluster is node-less:
// cluster discovery skips a distribution whose listing fails without reporting the result as
// incomplete, so a foreign cluster of the same name can be refused as unmanaged while an old
// remnant is still lying around. Letting that combination back into the normal delete flow would
// hand `provisioner.Delete` a cluster KSail does not own — exactly what the guard exists to stop.
//
// So the remnant only ever authorises removing the containers themselves. The provisioner is
// never reached from here, and the guard's refusal still stands for the cluster.
func handleUnmanagedDeleteTarget(
	cmd *cobra.Command,
	tmr timer.Timer,
	resolved *lifecycle.ResolvedClusterInfo,
	flags *deleteFlags,
	guardErr error,
) error {
	if !errors.Is(guardErr, ErrUnmanagedCluster) || !hasClusterRegistryRemnant(cmd, resolved) {
		return guardErr
	}

	remnant := clusterRegistryRemnant(cmd, resolved)

	notify.WriteMessage(notify.Message{
		Type: notify.WarningType,
		Content: "cluster " + resolved.ClusterName +
			" is not a KSail-managed cluster, but KSail-created registry containers for it are " +
			"still running; only those containers will be removed",
		Writer: cmd.OutOrStdout(),
	})

	// Removing containers (and, with --delete-storage, their volumes) is destructive, so it needs
	// the same consent as every other delete path. Returning early from the guard must not become
	// a way to skip the prompt.
	err := confirmAndReverifyDeleteTarget(cmd, flags, resolved, remnant, false)
	if err != nil {
		return err
	}

	cleanupRegistriesAfterDelete(cmd, tmr, resolved, flags.storage, nil)

	// Fail loudly when the cleanup did not actually clean up. Reporting success while the
	// containers and their host ports survive is precisely the defect this command is being
	// fixed for (#6286) — it must not reappear as the exit status of the fix.
	leftover := clusterRegistryRemnant(cmd, resolved)
	if len(leftover.Registries) > 0 {
		return fmt.Errorf("%w: %s", errRegistryRemnantSurvived, registryNames(leftover))
	}

	// KSail's own bookkeeping for this name goes with the containers. Left behind,
	// ~/.ksail/clusters/<name>/spec.json and ttl.json are inherited by the next cluster created
	// under the same name — a stale TTL or stale spec surfacing in update/info operations.
	//
	// This is the path that fires in practice for a node-less remnant: the guard refuses before
	// executeDelete is ever reached, so cleaning state only on the not-found path would miss it.
	stateErr := state.DeleteClusterState(resolved.ClusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(), "failed to clean up cluster state: %v", stateErr)
	}

	return nil
}

// executeDeleteTolerantOfMissingNodes deletes the cluster, treating an already-node-less cluster
// as success when KSail registry containers for it survive.
//
// Returning ErrClusterNotFound here would abort before post-deletion cleanup, so the containers
// the user is trying to remove would be skipped precisely when they are the only thing left (#6286).
func executeDeleteTolerantOfMissingNodes(
	cmd *cobra.Command,
	tmr timer.Timer,
	provisioner clusterprovisioner.Provisioner,
	resolved *lifecycle.ResolvedClusterInfo,
) error {
	err := executeDelete(cmd, tmr, provisioner, resolved)
	if err == nil {
		return nil
	}

	if !errors.Is(err, clustererr.ErrClusterNotFound) ||
		!hasClusterRegistryRemnant(cmd, resolved) {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.WarningType,
		Content: "cluster nodes are already gone; cleaning up its remaining resources",
		Writer:  cmd.OutOrStdout(),
	})

	// executeDelete returns before its own state cleanup on this path, so do it here. Left
	// behind, ~/.ksail/clusters/<name>/spec.json and ttl.json are inherited by the next cluster
	// of the same name — a stale TTL or stale spec surfacing in update/info operations.
	stateErr := state.DeleteClusterState(resolved.ClusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(), "failed to clean up cluster state: %v", stateErr)
	}

	return nil
}

// errRegistryRemnantSurvived reports that registry cleanup finished without removing everything.
var errRegistryRemnantSurvived = errors.New("registry containers could not be removed")

// registryNames renders a discovered set as a stable, comma-separated list.
func registryNames(d *mirrorregistry.DiscoveredRegistries) string {
	names := make([]string, 0, len(d.Registries))
	for _, reg := range d.Registries {
		names = append(names, reg.Name)
	}

	sort.Strings(names)

	return strings.Join(names, ", ")
}

// clusterRegistryRemnant returns the KSail-owned registry containers attributed to this cluster.
// Docker-provider only: no other provider creates them.
func clusterRegistryRemnant(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
) *mirrorregistry.DiscoveredRegistries {
	if resolved.Provider != v1alpha1.ProviderDocker {
		return &mirrorregistry.DiscoveredRegistries{}
	}

	return mirrorregistry.DiscoverClusterRegistryRemnant(
		cmd,
		resolved.ClusterName,
		getCleanupDeps(),
	)
}

// hasClusterRegistryRemnant reports whether KSail-owned registry containers for this cluster are
// still running. It is the evidence that KSail provisioned the cluster even after every node
// container is gone, and it never consults a Docker network (which a teardown may have destroyed).
//
// Docker-provider only: no other provider creates these containers.
func hasClusterRegistryRemnant(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
) bool {
	return len(clusterRegistryRemnant(cmd, resolved).Registries) > 0
}

// reportRegistryLeftovers names any KSail-owned registry container that survived cleanup.
//
// Teardown previously reported success unconditionally, so a partial teardown was
// indistinguishable from a complete one and the leaked containers (and the host ports they
// hold) went unnoticed until the next cluster collided with them (#6286).
func reportRegistryLeftovers(
	cmd *cobra.Command,
	clusterName string,
	cleanupDeps mirrorregistry.CleanupDependencies,
) {
	leftover := mirrorregistry.DiscoverClusterRegistryRemnant(cmd, clusterName, cleanupDeps)
	if len(leftover.Registries) == 0 {
		return
	}

	names := make([]string, 0, len(leftover.Registries))
	for _, reg := range leftover.Registries {
		names = append(names, reg.Name)
	}

	sort.Strings(names)

	notify.WriteMessage(notify.Message{
		Type: notify.ErrorType,
		Content: "these registry containers could not be removed and are still running: " +
			strings.Join(names, ", "),
		Writer: cmd.OutOrStdout(),
	})
}

// discoverDockerNodes discovers cluster node containers for Docker provider.
// Kind uses: {cluster}-control-plane, {cluster}-worker, etc.
// K3d uses: k3d-{cluster}-server-0, k3d-{cluster}-agent-0, etc.
// Talos uses: {cluster}-controlplane-*, {cluster}-worker-*.
func discoverDockerNodes(cmd *cobra.Command, clusterName string) []string {
	var nodes []string

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if IsClusterContainer(containerName, clusterName) {
			nodes = append(nodes, containerName)
		}

		return false // continue processing all containers
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

	// VCluster pattern: vcluster.cp.{cluster}
	if containerName == "vcluster.cp."+clusterName {
		return true
	}

	return false
}

// isKindClusterFromNodes determines if a cluster is a Kind cluster by checking
// if any of its nodes match Kind's container naming convention.
// This is used as a fallback when kubeconfig-based detection fails.
func isKindClusterFromNodes(nodes []string, clusterName string) bool {
	for _, node := range nodes {
		if matchesKindPattern(node, clusterName) {
			return true
		}
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

// isNumericString checks if a non-empty string contains only digits.
func isNumericString(s string) bool {
	if len(s) == 0 {
		return false
	}

	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// isCloudProviderKindContainer checks if a container name belongs to cloud-provider-kind.
func isCloudProviderKindContainer(name string) bool {
	return name == "ksail-cloud-provider-kind" || strings.HasPrefix(name, "cpk-")
}

// hasRemainingKindClusters checks if there are any Kind clusters remaining in Docker.
func hasRemainingKindClusters(cmd *cobra.Command) bool {
	return countKindClusters(cmd) > 0
}

// hasCloudProviderKindContainers checks if there are any cloud-provider-kind containers.
// This includes both the main ksail-cloud-provider-kind controller and cpk-* service containers.
func hasCloudProviderKindContainers(cmd *cobra.Command) bool {
	return len(listCloudProviderKindContainerNames(cmd)) > 0
}

// listCloudProviderKindContainerNames returns the names of all cloud-provider-kind containers.
// This includes both the main ksail-cloud-provider-kind controller and cpk-* service containers.
func listCloudProviderKindContainerNames(cmd *cobra.Command) []string {
	var names []string

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if isCloudProviderKindContainer(containerName) {
			names = append(names, containerName)
		}

		return false // continue processing all containers
	})

	return names
}

// countKindClusters counts the number of Kind clusters currently running.
// This is determined by counting containers with the -control-plane suffix.
func countKindClusters(cmd *cobra.Command) int {
	var count int

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if strings.HasSuffix(containerName, "-control-plane") {
			count++
		}

		return false // continue processing all containers
	})

	return count
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

	outputTimer := flags.MaybeTimer(cmd, tmr)

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
	return forEachContainer(
		cmd,
		func(dockerClient dockerclient.Client, ctr container.Summary, name string) error {
			if !isCloudProviderKindContainer(name) {
				return nil
			}

			err := dockerClient.ContainerRemove(
				cmd.Context(),
				ctr.ID,
				container.RemoveOptions{Force: true},
			)
			if err != nil {
				return fmt.Errorf("failed to remove container %s: %w", name, err)
			}

			return nil
		},
	)
}

// getLocalRegistryDeps returns the local registry dependencies, respecting any test overrides.
func getLocalRegistryDeps() localregistry.Dependencies {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	opts := []localregistry.Option{
		localregistry.WithDockerInvoker(invoker),
	}

	localRegistryServiceFactoryMu.RLock()

	factory := localRegistryServiceFactory

	localRegistryServiceFactoryMu.RUnlock()

	if factory != nil {
		opts = append(opts, localregistry.WithServiceFactory(factory))
	}

	return localregistry.NewDependencies(opts...)
}

// getCleanupDeps returns the cleanup dependencies for mirror registry operations.
func getCleanupDeps() mirrorregistry.CleanupDependencies {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	return mirrorregistry.CleanupDependencies{
		DockerInvoker:     invoker,
		LocalRegistryDeps: getLocalRegistryDeps(),
	}
}

// errNoClusterInfo is a sentinel error returned when no information is available
// from any source (provider API or Kubernetes API).
var errNoClusterInfo = errors.New("no cluster info available")

// errUnsupportedProvider is a sentinel error for unrecognized provider values.
var errUnsupportedProvider = errors.New("unsupported provider")

// errProviderNotConfigured is returned when provider credentials are missing.
var errProviderNotConfigured = errors.New("provider not configured")

// errContextNotFound is returned when a kubeconfig context cannot be found.
var errContextNotFound = errors.New("context not found in kubeconfig")
