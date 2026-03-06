package cluster

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	docker "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/svc/detector"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	specdiff "github.com/devantler-tech/ksail/v5/pkg/svc/diff"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewUpdateCmd creates the cluster update command.
// The update command applies configuration changes to a running cluster.
// It supports in-place updates where possible and falls back to recreation when necessary.
func NewUpdateCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update a cluster configuration",
		Long: `Update a Kubernetes cluster to match the current configuration.

This command applies changes from your ksail.yaml configuration to a running cluster.

For Talos clusters, many configuration changes can be applied in-place without
cluster recreation (e.g., network settings, kubelet config, registry mirrors).

For Kind/K3d clusters, in-place updates are more limited. Worker node scaling
is supported for K3d, but most other changes require cluster recreation.

Changes are classified into three categories:
  - In-Place: Applied without disruption
  - Reboot-Required: Applied but may require node reboots
  - Recreate-Required: Require full cluster recreation

Use --dry-run to preview changes without applying them.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		defaultClusterMutationFieldSelectors(),
	)

	registerMirrorRegistryFlag(cmd)
	registerNameFlag(cmd, cfgManager)

	cmd.Flags().Bool("force", false,
		"Skip confirmation prompt and proceed with cluster recreation")
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))

	cmd.Flags().BoolP("yes", "y", false,
		"Skip confirmation prompt (alias for --force)")

	cmd.Flags().Bool("dry-run", false,
		"Preview changes without applying them")
	_ = cfgManager.Viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleUpdateRunE)

	return cmd
}

// handleUpdateRunE executes the cluster update logic.
// It computes a diff between current and desired configuration, then applies
// changes in-place where possible, falling back to cluster recreation when necessary.
func handleUpdateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	deps.Timer.Start()

	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	// Load and validate configuration using shared helper
	ctx, clusterName, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	force := resolveForce(cfgManager.Viper.GetBool("force"), cmd.Flags().Lookup("yes"))

	// Create provisioner and verify cluster exists
	provisioner, err := createAndVerifyProvisioner(cmd, ctx, clusterName)
	if err != nil {
		return err
	}

	// Check if provisioner supports updates
	updater, supportsUpdate := provisioner.(clusterprovisioner.Updater)
	if !supportsUpdate {
		// Compute a spec-level diff to determine if there are actual changes
		// before falling back to recreation. No-op when nothing changed.
		specDiff := computeSpecOnlyDiff(cmd, ctx)
		if specDiff.TotalChanges() == 0 {
			notify.Infof(cmd.OutOrStdout(), "No changes detected")

			return nil
		}

		if cfgManager.Viper.GetBool("dry-run") {
			displayChangesSummary(cmd, specDiff)
			notify.Infof(
				cmd.OutOrStdout(),
				"Provisioner does not support in-place updates; "+
					"recreation would be required.\nDry run complete. No changes applied.",
			)

			return nil
		}

		return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
	}

	// Compute full diff; return error if current config cannot be retrieved
	// instead of falling back to recreation, which would be destructive.
	currentSpec, diff, diffErr := computeUpdateDiff(cmd, ctx, updater, clusterName)
	if diffErr != nil {
		return diffErr
	}

	// Display changes summary
	displayChangesSummary(cmd, diff)

	return applyOrReportChanges(cmd, cfgManager, ctx, deps, updater,
		clusterName, currentSpec, diff, outputTimer)
}

// createAndVerifyProvisioner creates a provisioner and verifies the cluster exists.
// It constructs a ComponentDetector from the cluster's kubeconfig and injects it
// into the provisioner so that GetCurrentConfig probes the live cluster.
//
// NOTE(limitation): If the user changes distribution in ksail.yaml (e.g., Kind → Talos), this
// creates a provisioner for the NEW distribution whose Exists() check won't find
// the old cluster, reporting "cluster does not exist" rather than detecting a
// distribution change. A proper fix would probe all provisioners for an existing
// cluster of any distribution. For now, users must run 'ksail cluster delete'
// before switching distributions.
func createAndVerifyProvisioner(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	clusterName string,
) (clusterprovisioner.Provisioner, error) {
	// Build a ComponentDetector scoped to the running cluster.
	componentDetector := buildComponentDetector(cmd, ctx)

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind:     ctx.KindConfig,
			K3d:      ctx.K3dConfig,
			Talos:    ctx.TalosConfig,
			VCluster: ctx.VClusterConfig,
		},
		ComponentDetector: componentDetector,
	}

	provisioner, _, err := factory.Create(cmd.Context(), ctx.ClusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provisioner: %w", err)
	}

	exists, err := provisioner.Exists(cmd.Context(), clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return nil, fmt.Errorf("%w: %q", clustererr.ErrClusterDoesNotExist, clusterName)
	}

	return provisioner, nil
}

// buildComponentDetector creates a ComponentDetector using the cluster's
// kubeconfig and Docker client. Returns nil when clients cannot be created
// (the provisioner will fall back to static defaults).
func buildComponentDetector(
	cmd *cobra.Command,
	ctx *localregistry.Context,
) *detector.ComponentDetector {
	helmClient, kubeconfig, err := setup.HelmClientForCluster(ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot create Helm client for component detection, using defaults: %v", err)

		return nil
	}

	k8sContext := ctx.ClusterCfg.Spec.Cluster.Connection.Context
	if k8sContext == "" {
		clusterName := resolveClusterNameFromContext(ctx)
		k8sContext = ctx.ClusterCfg.Spec.Cluster.Distribution.ContextName(clusterName)
	}

	k8sClientset, err := k8s.NewClientset(kubeconfig, k8sContext)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot create K8s clientset for component detection, using defaults: %v", err)

		return nil
	}

	// Docker client is optional — only needed for cloud-provider-kind detection.
	dockerClient, _ := docker.GetDockerClient()

	return detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
}

// computeUpdateDiff retrieves current config and computes the full diff.
// Returns an error if current config could not be retrieved; the caller should
// surface the error rather than silently recreating the cluster.
func computeUpdateDiff(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	updater clusterprovisioner.Updater,
	clusterName string,
) (*v1alpha1.ClusterSpec, *clusterupdate.UpdateResult, error) {
	currentSpec, err := updater.GetCurrentConfig(cmd.Context())
	if err != nil {
		return nil, nil, fmt.Errorf(
			"could not retrieve current cluster configuration: %w", err,
		)
	}

	diffEngine := specdiff.NewEngine(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	diff := diffEngine.ComputeDiff(currentSpec, &ctx.ClusterCfg.Spec.Cluster)

	provisionerDiff, diffErr := updater.DiffConfig(
		cmd.Context(), clusterName, currentSpec, &ctx.ClusterCfg.Spec.Cluster,
	)
	if diffErr == nil {
		specdiff.MergeProvisionerDiff(diff, provisionerDiff)
	}

	return currentSpec, diff, nil
}

// computeSpecOnlyDiff computes a spec-level diff using default values as
// the baseline current state. This is used for provisioners that do not
// implement the Updater interface (e.g., VCluster) to avoid blind recreation
// when there are no actual configuration changes.
func computeSpecOnlyDiff(
	cmd *cobra.Command,
	ctx *localregistry.Context,
) *clusterupdate.UpdateResult {
	currentSpec := clusterupdate.DefaultCurrentSpec(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	// Use component detection when available to get more accurate baseline.
	componentDetector := buildComponentDetector(cmd, ctx)
	if componentDetector != nil {
		detected, err := componentDetector.DetectComponents(
			cmd.Context(),
			ctx.ClusterCfg.Spec.Cluster.Distribution,
			ctx.ClusterCfg.Spec.Cluster.Provider,
		)
		if err == nil {
			currentSpec.CNI = detected.CNI
			currentSpec.CSI = detected.CSI
			currentSpec.MetricsServer = detected.MetricsServer
			currentSpec.LoadBalancer = detected.LoadBalancer
			currentSpec.CertManager = detected.CertManager
			currentSpec.PolicyEngine = detected.PolicyEngine
			currentSpec.GitOpsEngine = detected.GitOpsEngine
		}
	}

	// Apply GitOps local registry default AFTER detection so the detected
	// GitOps engine value is used to infer the default local registry address.
	clusterupdate.ApplyGitOpsLocalRegistryDefault(currentSpec)

	diffEngine := specdiff.NewEngine(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	return diffEngine.ComputeDiff(currentSpec, &ctx.ClusterCfg.Spec.Cluster)
}

// applyOrReportChanges handles dry-run, recreate-required, no-changes, and
// in-place change application.
func applyOrReportChanges(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	updater clusterprovisioner.Updater,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
) error {
	dryRun := cfgManager.Viper.GetBool("dry-run")
	force := resolveForce(cfgManager.Viper.GetBool("force"), cmd.Flags().Lookup("yes"))

	if dryRun {
		return reportDryRun(cmd, diff)
	}

	if diff.HasRecreateRequired() {
		return handleRecreateRequired(cmd, cfgManager, ctx, deps, clusterName, diff, force)
	}

	if !diff.HasInPlaceChanges() && !diff.HasRebootRequired() {
		notify.Infof(cmd.OutOrStdout(), "No changes detected")

		return nil
	}

	// Reboot-required changes are disruptive — require confirmation unless --force
	if diff.HasRebootRequired() && !confirm.ShouldSkipPrompt(force) {
		var block strings.Builder

		fmt.Fprintf(&block, "%d changes require node reboots:\n", len(diff.RebootRequired))

		for _, change := range diff.RebootRequired {
			fmt.Fprintf(&block, "  ⚠ %s: %s → %s. %s\n",
				change.Field, change.OldValue, change.NewValue, change.Reason,
			)
		}

		notify.Warningf(cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"Type \"yes\" to proceed with reboot-required changes: ",
		)

		if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
			notify.Infof(cmd.OutOrStdout(), "Update cancelled")

			return nil
		}
	}

	reconciler := newComponentReconciler(cmd, ctx.ClusterCfg)

	return applyInPlaceChanges(
		cmd, updater, reconciler, clusterName,
		currentSpec, ctx, diff, outputTimer,
	)
}

// reportDryRun prints a summary for dry-run mode and confirms no changes were applied.
func reportDryRun(cmd *cobra.Command, diff *clusterupdate.UpdateResult) error {
	if diff != nil && diff.TotalChanges() == 0 {
		notify.Infof(cmd.OutOrStdout(), "No changes detected")

		return nil
	}

	notify.Infof(cmd.OutOrStdout(), "Dry run complete. No changes applied.")

	return nil
}

// handleRecreateRequired warns about recreate-required changes and proceeds
// with recreation, prompting for confirmation unless --force is set.
func handleRecreateRequired(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	force bool,
) error {
	var block strings.Builder

	fmt.Fprintf(&block, "%d changes require cluster recreation:\n", len(diff.RecreateRequired))

	for _, change := range diff.RecreateRequired {
		fmt.Fprintf(&block, "  ✗ %s: cannot change from %s to %s in-place. %s\n",
			change.Field, change.OldValue, change.NewValue, change.Reason,
		)
	}

	notify.Warningf(cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

	return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
}

// applyInPlaceChanges applies provisioner-level and component-level changes in-place.
func applyInPlaceChanges(
	cmd *cobra.Command,
	updater clusterprovisioner.Updater,
	reconciler *componentReconciler,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	ctx *localregistry.Context,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
) error {
	updateOpts := clusterupdate.UpdateOptions{
		DryRun:        false,
		RollingReboot: true,
	}

	notify.Titlef(cmd.OutOrStdout(), "🔄", "Applying changes...")

	// Apply provisioner-level changes (node scaling, Talos config, etc.)
	result, err := updater.Update(
		cmd.Context(),
		clusterName,
		currentSpec,
		&ctx.ClusterCfg.Spec.Cluster,
		updateOpts,
	)
	if err != nil {
		return fmt.Errorf("failed to apply updates: %w", err)
	}

	// Apply component-level changes (CNI, CSI, cert-manager, etc.)
	componentErr := reconciler.reconcileComponents(cmd.Context(), diff, result)

	// Display results
	if len(result.AppliedChanges) > 0 {
		notify.SuccessWithTimerf(cmd.OutOrStdout(), outputTimer,
			"applied %d changes successfully", len(result.AppliedChanges),
		)
	}

	if len(result.FailedChanges) > 0 {
		var failBlock strings.Builder

		fmt.Fprintf(&failBlock, "%d changes failed to apply:\n", len(result.FailedChanges))

		for _, change := range result.FailedChanges {
			fmt.Fprintf(&failBlock, "  - %s: %s\n", change.Field, change.Reason)
		}

		notify.Errorf(cmd.OutOrStderr(), strings.TrimRight(failBlock.String(), "\n"))
	}

	if componentErr != nil {
		return fmt.Errorf("some component changes failed to apply: %w", componentErr)
	}

	return nil
}

// displayChangesSummary outputs a human-readable summary of configuration changes
// as a single grouped block showing before → after values for each field.
func displayChangesSummary(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	totalChanges := len(diff.InPlaceChanges) + len(diff.RebootRequired) + len(diff.RecreateRequired)

	if totalChanges == 0 {
		return
	}

	notify.Titlef(cmd.OutOrStdout(), "🔍", "Change summary")

	var block strings.Builder

	fmt.Fprintf(&block, "Detected %d configuration changes:", totalChanges)

	for _, change := range diff.InPlaceChanges {
		fmt.Fprintf(&block, "\n  🟢 %s: %s → %s", change.Field, change.OldValue, change.NewValue)
	}

	for _, change := range diff.RebootRequired {
		fmt.Fprintf(&block, "\n  🟡 %s: %s → %s", change.Field, change.OldValue, change.NewValue)
	}

	for _, change := range diff.RecreateRequired {
		fmt.Fprintf(&block, "\n  🔴 %s: %s → %s", change.Field, change.OldValue, change.NewValue)
	}

	notify.Infof(cmd.OutOrStdout(), block.String())
}

// confirmRecreate prompts the user to confirm cluster recreation unless --force is set.
// It returns true if the update should proceed (confirmed or forced), and false if the user cancels.
func confirmRecreate(cmd *cobra.Command, clusterName string, force bool) bool {
	if confirm.ShouldSkipPrompt(force) {
		return true
	}

	var prompt strings.Builder

	prompt.WriteString(
		"Update will delete and recreate the cluster.\n",
	)
	prompt.WriteString("All workloads and data will be lost.")

	notify.Warningf(cmd.OutOrStderr(), "%s", prompt.String())

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"Type \"yes\" to proceed with updating cluster %q: ", clusterName,
	)

	if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
		notify.Infof(cmd.OutOrStdout(), "Update cancelled")

		return false
	}

	return true
}

// executeRecreateFlow performs the delete + create flow with confirmation.
func executeRecreateFlow(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	force bool,
) error {
	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	if !confirmRecreate(cmd, clusterName, force) {
		return nil
	}

	// Create provisioner for delete
	factory := newProvisionerFactory(ctx)

	provisioner, _, err := factory.Create(cmd.Context(), ctx.ClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Disconnect registries from Docker network before deletion.
	// Required for distributions like VCluster and Talos because their provisioners
	// destroy the Docker network during deletion, which fails if containers are
	// still connected. Registries are reused on recreate, so only disconnect is needed.
	if ctx.ClusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderDocker {
		disconnectRegistriesBeforeDelete(cmd, &clusterdetector.Info{
			Distribution: ctx.ClusterCfg.Spec.Cluster.Distribution,
			ClusterName:  clusterName,
		})
	}

	// Execute delete
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Emoji:   "🗑️",
		Content: "deleting existing cluster",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err = provisioner.Delete(cmd.Context(), clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete existing cluster: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	// Execute create using shared workflow
	return runClusterCreationWorkflow(cmd, cfgManager, ctx, deps)
}

// resolveForce returns true if the viper-resolved force flag is set,
// or if the --yes flag was explicitly passed on the command line.
// This consolidates the --force/--yes alias logic into one place.
func resolveForce(viperForce bool, yesFlag *pflag.Flag) bool {
	return viperForce || (yesFlag != nil && yesFlag.Changed)
}
