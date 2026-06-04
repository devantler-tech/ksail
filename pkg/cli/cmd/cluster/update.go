package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfighook"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	argocdclient "github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"
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

Changes are classified into the following categories:
  - In-Place: Applied without disruption
  - Reboot-Required: Applied but may require node reboots
  - Wipe-Required: Requires wiping node partitions (e.g. disk encryption
    migration); requires --force
  - Rolling-Recreate: Nodes are replaced one at a time (e.g. a Talos × Hetzner
    server-type change); requires confirmation (or --force to skip the prompt)
  - Recreate-Required: Require full cluster recreation

Use --dry-run to preview changes without applying them.
Use --output json to emit a machine-readable diff for CI/MCP consumption.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cfgManager := setupMutationCmdFlags(cmd)

	cmd.Flags().Bool("force", false,
		"Skip confirmation prompt and proceed with cluster recreation")
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))

	cmd.Flags().BoolP("yes", "y", false,
		"Skip confirmation prompt (alias for --force)")

	cmd.Flags().Bool("dry-run", false,
		"Preview changes without applying them")
	_ = cfgManager.Viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))

	cmd.Flags().String("output", outputFormatText,
		"Output format: text (default) or json (machine-readable, for CI/MCP)")

	cmd.Flags().Bool("update-kubernetes", false,
		"Upgrade Kubernetes to the latest stable version available in the OCI registry")
	_ = cfgManager.Viper.BindPFlag("update-kubernetes", cmd.Flags().Lookup("update-kubernetes"))

	cmd.Flags().Bool("update-distribution", false,
		"Upgrade the distribution to the latest stable version available in the OCI registry")
	_ = cfgManager.Viper.BindPFlag("update-distribution", cmd.Flags().Lookup("update-distribution"))

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleUpdateRunE)

	return cmd
}

// handleUpdateRunE executes the cluster update logic.
// It computes a diff between current and desired configuration, then applies
// changes in-place where possible, falling back to cluster recreation when necessary.
//
//nolint:cyclop,funlen // orchestration function with sequential lifecycle phases
func handleUpdateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	err := validateOutputFormat(cmd)
	if err != nil {
		return err
	}

	deps.Timer.Start()

	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	// Load and validate configuration using shared helper
	ctx, clusterName, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	applyOIDCExtraScopeFlag(cmd, ctx.ClusterCfg)
	applyAllowedCIDRsFlag(cmd, ctx.ClusterCfg)

	// Re-validate OIDC after merging CLI scope flags which can change ExtraScopes
	err = v1alpha1.ValidateOIDCConfig(&ctx.ClusterCfg.Spec.Cluster.OIDC)
	if err != nil {
		return fmt.Errorf("OIDC configuration: %w", err)
	}

	// Validate allowed CIDRs after merging CLI flags
	err = v1alpha1.ValidateAllowedCIDRs(ctx.ClusterCfg.Spec.Provider.Hetzner.AllowedCIDRs)
	if err != nil {
		return fmt.Errorf("allowed CIDRs configuration: %w", err)
	}

	force := resolveForce(cfgManager.Viper.GetBool("force"), cmd.Flags().Lookup("yes"))

	// Create provisioner and verify cluster exists
	provisioner, err := createAndVerifyProvisioner(cmd, ctx, clusterName)
	if err != nil {
		return err
	}

	// Handle version upgrades when requested
	updateK8s := cfgManager.Viper.GetBool("update-kubernetes")
	updateDist := cfgManager.Viper.GetBool("update-distribution")

	if updateK8s || updateDist {
		recreated, err := handleVersionUpgrades(
			cmd, cfgManager, ctx, deps, provisioner,
			clusterName, updateK8s, updateDist, force,
		)
		if err != nil {
			return err
		}
		// If the cluster was recreated, skip the regular update flow —
		// recreation already started a fresh cluster at the target version.
		if recreated {
			return nil
		}
	}

	// Check if provisioner supports updates
	updater, supportsUpdate := provisioner.(clusterprovisioner.Updater)
	if !supportsUpdate {
		// Compute a spec-level diff to determine if there are actual changes
		// before falling back to recreation. No-op when nothing changed.
		specDiff := computeSpecOnlyDiff(cmd, ctx)
		if specDiff.TotalChanges() == 0 {
			// Surface any unknown-baseline components so the user sees that the
			// current state could not be read, then exit without recreating.
			if specDiff.HasUnknownBaseline() {
				displayChangesSummary(cmd, specDiff)
			}

			reportNoApplicableChanges(cmd, specDiff)

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

// handleVersionUpgrades orchestrates Kubernetes and/or distribution version upgrades.
// It discovers available versions from OCI registries, computes an ordered upgrade
// path (oldest→latest), and applies each step sequentially. If any step fails, the
// cluster remains at the last successful version with actionable feedback.
//
// For distributions that require recreation (Kind, K3d, VCluster), the upgrade
// skips directly to the latest available version and recreates the cluster once,
// since there is no running state to preserve between intermediate versions.
//
// When both flags are set, distribution upgrades run first (the distribution
// runtime must support the target Kubernetes version).
//
//nolint:cyclop,funlen // orchestration function with distinct sequential phases
func handleVersionUpgrades(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	provisioner clusterprovisioner.Provisioner,
	clusterName string,
	updateK8s, updateDist, force bool,
) (bool, error) {
	upgrader, ok := provisioner.(clusterupdate.Upgrader)
	if !ok {
		return false, fmt.Errorf("%w: %s",
			clustererr.ErrUpgraderNotSupported, ctx.ClusterCfg.Spec.Cluster.Distribution)
	}

	currentVersions, err := upgrader.GetCurrentVersions(cmd.Context(), clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to get current versions: %w", err)
	}

	resolver := versionresolver.NewOCIResolver()
	dryRun := cfgManager.Viper.GetBool("dry-run")
	recreated := false

	// Distribution upgrades first (runtime must support K8s version).
	if updateDist { //nolint:nestif // sequential phase with version refresh guard
		// Respect Talos version pin: when spec.cluster.talos.version is set,
		// cap the distribution upgrade at the pinned version instead of
		// discovering the latest from OCI. Skip entirely if already at the pin.
		if ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos &&
			ctx.ClusterCfg.Spec.Cluster.Talos.Version != "" {
			err := executePinnedDistributionUpgrade(
				cmd, upgrader, clusterName,
				ctx.ClusterCfg.Spec.Cluster.Talos.Version,
				currentVersions.DistributionVersion, dryRun,
			)
			if err != nil {
				return false, err
			}
		} else {
			stepRecreated, err := executeVersionUpgrade(
				cmd, cfgManager, ctx, deps, upgrader, resolver, clusterName,
				"distribution", upgrader.DistributionImageRef(),
				currentVersions.DistributionVersion, upgrader.VersionSuffix(),
				upgrader.UpgradeDistribution, force, dryRun,
			)
			if err != nil {
				return false, err
			}

			if stepRecreated {
				recreated = true
			}
		}

		// Re-fetch versions after distribution upgrade since recreation may
		// have changed the Kubernetes version too (Kind/K3d bundle both).
		if updateK8s && !dryRun && !recreated {
			currentVersions, err = upgrader.GetCurrentVersions(cmd.Context(), clusterName)
			if err != nil {
				return false, fmt.Errorf(
					"failed to refresh versions after distribution upgrade: %w", err,
				)
			}
		}
	}

	// Then Kubernetes upgrades. Skip if we already recreated the cluster
	// (recreation picks up the latest configured version for both).
	if updateK8s && !recreated {
		stepRecreated, err := executeVersionUpgrade(
			cmd, cfgManager, ctx, deps, upgrader, resolver, clusterName,
			"Kubernetes", upgrader.KubernetesImageRef(),
			currentVersions.KubernetesVersion, upgrader.VersionSuffix(),
			upgrader.UpgradeKubernetes, force, dryRun,
		)
		if err != nil {
			return false, err
		}

		if stepRecreated {
			recreated = true
		}
	}

	return recreated, nil
}

// upgradeFunc is the signature for UpgradeKubernetes / UpgradeDistribution.
type upgradeFunc func(ctx context.Context, clusterName, fromVersion, toVersion string) error

// executeVersionUpgrade discovers available versions, computes an upgrade path,
// and applies each step. For distributions requiring recreation, it jumps to the
// latest version and triggers a single recreate.
//
//nolint:cyclop,funlen // sequential upgrade logic with distinct phases
func executeVersionUpgrade(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	upgrader clusterupdate.Upgrader,
	resolver versionresolver.Resolver,
	clusterName string,
	upgradeType string,
	imageRef string,
	currentVersion string,
	suffix string,
	applyFn upgradeFunc,
	force, dryRun bool,
) (bool, error) {
	if imageRef == "" {
		notify.Infof(cmd.OutOrStdout(),
			"No separate %s image for this distribution; "+
				"use --update-kubernetes to upgrade",
			upgradeType)

		return false, nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Emoji:   "🔍",
		Content: fmt.Sprintf("discovering available %s versions from %s", upgradeType, imageRef),
		Writer:  cmd.OutOrStdout(),
	})

	path, err := versionresolver.ComputeUpgradePath(
		cmd.Context(), resolver, imageRef, currentVersion, suffix,
	)
	if err != nil {
		if errors.Is(err, versionresolver.ErrNoUpgradesAvailable) {
			notify.Infof(cmd.OutOrStdout(),
				"%s is already at the latest stable version (%s)", upgradeType, currentVersion)

			return false, nil
		}

		return false, fmt.Errorf("failed to compute %s upgrade path: %w", upgradeType, err)
	}

	// Display upgrade path
	notify.WriteMessage(notify.Message{
		Type:  notify.InfoType,
		Emoji: "📋",
		Content: fmt.Sprintf("%s upgrade path: %s → %s (%d step(s))",
			upgradeType, currentVersion, path[len(path)-1].Version.Original, len(path)),
		Writer: cmd.OutOrStdout(),
	})

	if dryRun {
		for i, step := range path {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, step.Version.Original)
		}

		notify.Infof(cmd.OutOrStdout(), "Dry run complete. No %s upgrades applied.", upgradeType)

		return false, nil
	}

	// Determine upgrade mechanism by attempting the first upgrade step.
	// Recreation-based distributions (Kind/K3d/VCluster) return ErrRecreationRequired
	// immediately without modifying the cluster, so we jump to the latest version
	// and recreate once. For rolling-upgrade distributions (Talos), the first step
	// is actually applied.
	targetVersion := path[len(path)-1].Version.Original

	probeErr := applyFn(cmd.Context(), clusterName, currentVersion, path[0].Version.Original)
	if probeErr != nil && errors.Is(probeErr, clustererr.ErrUpgradeSkipped) {
		notify.Infof(cmd.OutOrStdout(), "%s upgrade skipped: %v", upgradeType, probeErr)

		return false, nil
	}

	if probeErr != nil && errors.Is(probeErr, clustererr.ErrRecreationRequired) {
		prepErr := upgrader.PrepareConfigForVersion(upgradeType, targetVersion)
		if prepErr != nil {
			return false, fmt.Errorf(
				"failed to prepare config for %s %s: %w",
				upgradeType, targetVersion, prepErr,
			)
		}

		return true, handleRecreationUpgrade(cmd, cfgManager, ctx, deps, clusterName,
			upgradeType, currentVersion, targetVersion, force)
	}

	// Rolling upgrade (Talos): first step already applied by the probe, continue with the rest.
	if probeErr != nil {
		return false, fmt.Errorf(
			"%s upgrade failed at step 1/%d (%s → %s), cluster is still running %s: %w",
			upgradeType, len(path), currentVersion, path[0].Version.Original,
			currentVersion, probeErr,
		)
	}

	notify.WriteMessage(notify.Message{
		Type:  notify.SuccessType,
		Emoji: "⬆️",
		Content: fmt.Sprintf("%s upgraded: step 1/%d → %s",
			upgradeType, len(path), path[0].Version.Original),
		Writer: cmd.OutOrStdout(),
	})

	for stepIdx := 1; stepIdx < len(path); stepIdx++ {
		step := path[stepIdx]
		prevVersion := path[stepIdx-1].Version.Original

		notify.WriteMessage(notify.Message{
			Type:  notify.ActivityType,
			Emoji: "⬆️",
			Content: fmt.Sprintf("upgrading %s: step %d/%d (%s → %s)",
				upgradeType, stepIdx+1, len(path), prevVersion, step.Version.Original),
			Writer: cmd.OutOrStdout(),
		})

		applyErr := applyFn(
			cmd.Context(), clusterName, prevVersion, step.Version.Original,
		)
		if applyErr != nil {
			notify.Warningf(cmd.OutOrStderr(),
				"%s upgrade to %s failed (cluster is at %s): %v",
				upgradeType, step.Version.Original, prevVersion, applyErr)

			return false, fmt.Errorf(
				"%s upgrade failed at step %d/%d (%s → %s), cluster is running %s: %w",
				upgradeType, stepIdx+1, len(path), prevVersion, step.Version.Original,
				prevVersion, applyErr,
			)
		}

		notify.WriteMessage(notify.Message{
			Type:  notify.SuccessType,
			Emoji: "⬆️",
			Content: fmt.Sprintf("%s upgraded: step %d/%d → %s",
				upgradeType, stepIdx+1, len(path), step.Version.Original),
			Writer: cmd.OutOrStdout(),
		})
	}

	notify.WriteMessage(notify.Message{
		Type: notify.SuccessType,
		Content: fmt.Sprintf(
			"%s upgrade complete: %s → %s",
			upgradeType, currentVersion, targetVersion,
		),
		Writer: cmd.OutOrStdout(),
	})

	return false, nil
}

// handleRecreationUpgrade handles version upgrades for distributions that require
// cluster recreation (Kind, K3d, VCluster). It confirms with the user, then
// recreates the cluster which will pick up the latest configured version.
func handleRecreationUpgrade(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	upgradeType string,
	currentVersion, targetVersion string,
	force bool,
) error {
	notify.WriteMessage(notify.Message{
		Type:  notify.InfoType,
		Emoji: "🔄",
		Content: fmt.Sprintf(
			"%s upgrade from %s to %s requires cluster recreation",
			upgradeType, currentVersion, targetVersion,
		),
		Writer: cmd.OutOrStdout(),
	})

	return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
}

// pinnedVersionSkipReason indicates why a pinned version upgrade was skipped.
type pinnedVersionSkipReason int

const (
	pinnedVersionProceed     pinnedVersionSkipReason = iota // No skip — proceed with upgrade.
	pinnedVersionAlreadyAtIt                                // Cluster is already at the pinned version.
	pinnedVersionNewer                                      // Cluster is newer than the pinned version.
)

// executePinnedDistributionUpgrade handles Talos distribution upgrades when a version pin is set.
// It normalizes the pinned version, validates it is a parseable semver, guards against downgrades,
// and either skips (already at pin), applies the upgrade, or reports a dry-run summary.
//
//nolint:funlen // Sequential upgrade orchestration with distinct skip/dry-run/apply phases.
func executePinnedDistributionUpgrade(
	cmd *cobra.Command,
	upgrader clusterupdate.Upgrader,
	clusterName string,
	rawPinnedVersion string,
	currentVersion string,
	dryRun bool,
) error {
	pinnedVersion, skipReason, err := normalizePinnedVersion(rawPinnedVersion, currentVersion)
	if err != nil {
		return err
	}

	if skipReason == pinnedVersionAlreadyAtIt {
		notify.Infof(cmd.OutOrStdout(),
			"distribution is already at pinned version %s", pinnedVersion)

		return nil
	}

	if skipReason == pinnedVersionNewer {
		notify.Infof(cmd.OutOrStdout(),
			"cluster is at %s which is newer than pinned version %s; skipping downgrade",
			currentVersion, pinnedVersion)

		return nil
	}

	notify.WriteMessage(notify.Message{
		Type:  notify.InfoType,
		Emoji: "📌",
		Content: fmt.Sprintf("distribution upgrade capped at pinned version %s (current: %s)",
			pinnedVersion, currentVersion),
		Writer: cmd.OutOrStdout(),
	})

	if dryRun {
		notify.Infof(cmd.OutOrStdout(),
			"Dry run complete. Would upgrade distribution to pinned version %s.", pinnedVersion)

		return nil
	}

	upgradeErr := upgrader.UpgradeDistribution(
		cmd.Context(), clusterName, currentVersion, pinnedVersion,
	)
	if upgradeErr != nil {
		if errors.Is(upgradeErr, clustererr.ErrUpgradeSkipped) {
			notify.Infof(cmd.OutOrStdout(), "distribution upgrade skipped: %v", upgradeErr)

			return nil
		}

		return fmt.Errorf("distribution upgrade to pinned version %s failed: %w",
			pinnedVersion, upgradeErr)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "distribution upgraded to pinned version " + pinnedVersion,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// normalizePinnedVersion validates and normalizes a pinned Talos version.
// Returns the normalized version string and a skip reason. When the skip reason
// is pinnedVersionProceed, the caller should proceed with the upgrade.
// Returns an error if the version is empty or not a valid semver tag.
func normalizePinnedVersion(
	rawPinnedVersion, currentVersion string,
) (string, pinnedVersionSkipReason, error) {
	rawPinnedVersion = strings.TrimSpace(rawPinnedVersion)
	if rawPinnedVersion == "" {
		return "", pinnedVersionProceed, ErrEmptyPinnedVersion
	}

	pinnedVersion := rawPinnedVersion
	if !strings.HasPrefix(pinnedVersion, "v") {
		pinnedVersion = "v" + pinnedVersion
	}

	// Validate the pinned version is a parseable semver tag.
	pinned, pinErr := versionresolver.ParseVersion(pinnedVersion)
	if pinErr != nil {
		return "", pinnedVersionProceed, fmt.Errorf(
			"invalid pinned Talos version %q: %w", rawPinnedVersion, pinErr,
		)
	}

	if currentVersion == pinnedVersion {
		return pinnedVersion, pinnedVersionAlreadyAtIt, nil
	}

	// Guard against downgrades: skip if the cluster is already newer than the pin.
	current, curErr := versionresolver.ParseVersion(currentVersion)
	if curErr == nil && pinned.Less(current) {
		return pinnedVersion, pinnedVersionNewer, nil
	}

	return pinnedVersion, pinnedVersionProceed, nil
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
	// Create provisioner without component detector first.
	// The detector requires a kubeconfig, which may not exist yet for
	// remote providers (Omni). We build the detector after refreshing
	// the kubeconfig below.
	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind:     ctx.KindConfig,
			K3d:      ctx.K3dConfig,
			Talos:    ctx.TalosConfig,
			VCluster: ctx.VClusterConfig,
			KWOK:     ctx.KWOKConfig,
		},
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

	// Refresh kubeconfig from the remote provider if supported.
	// This ensures the kubeconfig is available for component detection
	// and subsequent Helm operations (CNI, GitOps installation).
	refresher, ok := provisioner.(clusterprovisioner.KubeconfigRefresher)
	if ok {
		err = refreshAndVerifyKubeconfig(cmd, refresher, ctx.ClusterCfg, clusterName)
		if err != nil {
			return nil, err
		}
	}

	// Build a ComponentDetector scoped to the running cluster.
	// Now that kubeconfig is ensured, the detector can connect.
	componentDetector := buildComponentDetector(cmd, ctx)

	if aware, ok := provisioner.(clusterprovisioner.ComponentDetectorAware); ok {
		aware.SetComponentDetector(componentDetector)
	}

	return provisioner, nil
}

//nolint:gochecknoglobals // dependency injection for tests
var isKubeconfigStaleFunc = kubeconfighook.IsKubeconfigStale

// refreshAndVerifyKubeconfig ensures a valid kubeconfig is available for
// downstream Helm/GitOps operations. The refresh is best-effort:
//
//   - If a valid kubeconfig already exists at the expected path (file present and
//     not rejected by the K8s API with an auth error), the refresh is skipped
//     entirely. This handles CI runners where a kubeconfig is pre-loaded from a
//     secret and the talosconfig is absent or empty. Note: the staleness check
//     considers the kubeconfig valid when the API server is unreachable (timeout,
//     connection refused) — this is intentional because the credentials may still
//     be valid and refreshing would also fail in that scenario.
//
//   - If the kubeconfig is missing or stale, the provisioner's KubeconfigRefresher
//     is called. A hard error is returned only when the refresh fails AND no file
//     exists as a fallback. If a file already existed (even if stale), a warning is
//     emitted and the command proceeds — the downstream operations may still succeed.
//
// The existence-and-validity check uses a lightweight K8s ServerVersion probe
// (3 s timeout) so it never blocks meaningful work.
func refreshAndVerifyKubeconfig(
	cmd *cobra.Command,
	refresher clusterprovisioner.KubeconfigRefresher,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	kubeconfigPath, pathErr := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if pathErr != nil {
		return fmt.Errorf("failed to resolve kubeconfig path: %w", pathErr)
	}

	kubeconfigContext := clusterCfg.Spec.Cluster.Connection.Context

	_, statErr := os.Stat(kubeconfigPath)
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to stat kubeconfig at %q: %w", kubeconfigPath, statErr)
	}

	fileExists := statErr == nil

	// If the kubeconfig file is present and not stale (i.e., loadable and
	// not rejected by the API server with an auth error), skip the refresh.
	// Note: IsKubeconfigStale returns false when the API is unreachable
	// (timeout, connection refused) — this is intentional because the
	// kubeconfig credentials may still be valid; refreshing would also fail
	// in that scenario.
	if fileExists && !isKubeconfigStaleFunc(kubeconfigPath, kubeconfigContext) {
		return nil
	}

	err := refresher.RefreshKubeconfig(cmd.Context(), clusterName)
	if err != nil {
		if fileExists {
			// The kubeconfig file was present before we tried (though possibly
			// stale). We cannot refresh it (e.g., talosconfig is missing on an
			// ephemeral CI runner), but the file may still work. Warn and let
			// the downstream operations decide.
			notify.Warningf(cmd.OutOrStderr(), "failed to refresh kubeconfig: %v", err)

			return nil
		}

		return fmt.Errorf("failed to refresh kubeconfig: %w", err)
	}

	_, statErr = os.Stat(kubeconfigPath)
	if statErr != nil {
		return fmt.Errorf(
			"kubeconfig not available after refresh for %s provider at %q: %w",
			clusterCfg.Spec.Cluster.Provider, kubeconfigPath, statErr,
		)
	}

	return nil
}

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
	currentSpec, currentProvider, err := updater.GetCurrentConfig(cmd.Context(), clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"could not retrieve current cluster configuration: %w", err,
		)
	}

	diffEngine := specdiff.NewEngine(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	diff := diffEngine.ComputeDiff(
		currentSpec, &ctx.ClusterCfg.Spec.Cluster,
		currentProvider, &ctx.ClusterCfg.Spec.Provider,
	)

	provisionerDiff, diffErr := updater.DiffConfig(
		cmd.Context(), clusterName, currentSpec, &ctx.ClusterCfg.Spec.Cluster,
	)
	if diffErr == nil {
		specdiff.MergeProvisionerDiff(diff, provisionerDiff)
	}

	// Check for workload tag drift (stale GitOps sync ref)
	checkWorkloadTagDrift(cmd, ctx, diffEngine, diff)

	// Check for Flux distribution-version drift (spec.workload.flux.distributionVersion)
	checkFluxDistributionVersionDrift(cmd, ctx, diffEngine, diff)

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
	if componentDetector == nil {
		// The detector's clients (Helm/K8s) could not be constructed, so live
		// cluster state cannot be read at all. Treat this like a detection
		// failure and mark the baseline Unknown rather than fabricating a
		// confident diff against defaults. buildComponentDetector already logged
		// the underlying reason.
		clusterupdate.MarkComponentsUnknown(currentSpec)
	} else {
		detected, err := componentDetector.DetectComponents(
			cmd.Context(),
			ctx.ClusterCfg.Spec.Cluster.Distribution,
			ctx.ClusterCfg.Spec.Cluster.Provider,
		)
		if err != nil {
			notify.Warningf(
				cmd.ErrOrStderr(),
				"Cannot detect live cluster components; baseline shown as Unknown: %v",
				err,
			)
			// Mark detector-derived fields unknown so the diff surfaces them as
			// "Unknown" rather than a confident diff against default values.
			clusterupdate.MarkComponentsUnknown(currentSpec)
		} else {
			currentSpec.CNI = detected.CNI
			currentSpec.CSI = detected.CSI
			currentSpec.MetricsServer = detected.MetricsServer
			currentSpec.LoadBalancer = detected.LoadBalancer
			currentSpec.CertManager = detected.CertManager
			currentSpec.PolicyEngine = detected.PolicyEngine
			currentSpec.GitOpsEngine = detected.GitOpsEngine
			currentSpec.Autoscaler.Node = detected.Autoscaler.Node
		}
	}

	diffEngine := specdiff.NewEngine(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	// Build the target spec, applying any distribution-specific overrides so
	// the diff reflects what the distribution will actually install rather than
	// what the user requested.  Copying avoids mutating the shared context.
	targetSpec := ctx.ClusterCfg.Spec.Cluster
	applyDistributionSpecOverrides(&targetSpec)

	diff := diffEngine.ComputeDiff(
		currentSpec,
		&targetSpec,
		nil,
		&ctx.ClusterCfg.Spec.Provider,
	)

	// Check for workload tag drift (stale GitOps sync ref)
	checkWorkloadTagDrift(cmd, ctx, diffEngine, diff)

	// Check for Flux distribution-version drift (spec.workload.flux.distributionVersion)
	checkFluxDistributionVersionDrift(cmd, ctx, diffEngine, diff)

	return diff
}

// applyDistributionSpecOverrides normalises a ClusterSpec by clearing fields
// that the given distribution will never install, so that update dry-runs do
// not report spurious "install X" changes for features that are silently skipped
// at cluster-creation time.
func applyDistributionSpecOverrides(spec *v1alpha1.ClusterSpec) {
	if spec.Distribution == v1alpha1.DistributionKWOK {
		// KWOK cannot run admission-webhook servers (simulated pods have no
		// real network), so policy engines are always skipped at creation time.
		// Treat PolicyEngine as None so the diff stays clean.
		spec.PolicyEngine = v1alpha1.PolicyEngineNone

		// The flux-operator pod is simulated and never installs Flux CRDs, so
		// Flux is always skipped at creation time (GetComponentRequirements sets
		// NeedsFlux=false for KWOK). Treat GitOpsEngine as None for Flux so that
		// update dry-runs do not report spurious "install Flux" changes.
		if spec.GitOpsEngine == v1alpha1.GitOpsEngineFlux {
			spec.GitOpsEngine = v1alpha1.GitOpsEngineNone
		}

		// NeedsLoadBalancerInstall always returns false for KWOK (no real network
		// dataplane). Normalise LoadBalancer to Disabled so that the update diff
		// sees Disabled on both sides and reports no change.
		spec.LoadBalancer = v1alpha1.LoadBalancerDisabled

		// KWOK runs simulated pods with no real network dataplane, so CNI plugins
		// (Calico, Cilium, etc.) are never installed at creation time. Normalise CNI
		// to Default so that update dry-runs do not report spurious CNI change diffs.
		spec.CNI = v1alpha1.CNIDefault

		// CSI node-plugin pods are simulated and never become Ready on KWOK.
		// Normalise CSI to Disabled so update dry-runs do not report a spurious
		// "install CSI" change for a feature that is silently skipped at creation.
		spec.CSI = v1alpha1.CSIDisabled

		// cert-manager admission webhook pods are simulated on KWOK and never run
		// real TLS logic. Normalise CertManager to Disabled for the same reason.
		spec.CertManager = v1alpha1.CertManagerDisabled
	}
}

// checkWorkloadTagDrift queries the running GitOps sync resource for its current
// tag (FluxInstance.sync.ref or ArgoCD Application.targetRevision) and compares
// it against the desired tag from configuration. If they differ, an in-place
// change is appended to the diff result. This detects stale sync refs left by
// pre-v6.7.1 cluster creation.
// Errors during cluster queries are logged as warnings and skipped — they should
// not block the rest of the update.
func checkWorkloadTagDrift(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	diffEngine *specdiff.Engine,
	diff *clusterupdate.UpdateResult,
) {
	gitOpsEngine := ctx.ClusterCfg.Spec.Cluster.GitOpsEngine
	if gitOpsEngine == v1alpha1.GitOpsEngineNone || gitOpsEngine == "" {
		return
	}

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot resolve kubeconfig path for workload tag drift detection: %v", err)

		return
	}

	desiredTag := fluxinstaller.ResolveDesiredTag(ctx.ClusterCfg)

	var currentTag string

	switch gitOpsEngine { //nolint:exhaustive // None/empty already filtered above
	case v1alpha1.GitOpsEngineFlux:
		currentTag, err = fluxinstaller.GetCurrentSyncRef(cmd.Context(), kubeconfigPath)
	case v1alpha1.GitOpsEngineArgoCD:
		currentTag, err = getCurrentArgoCDTargetRevision(cmd.Context(), kubeconfigPath)
	default:
		return
	}

	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot query current GitOps sync ref for drift detection: %v", err)

		return
	}

	// Empty current tag means the resource does not exist yet — no drift to fix.
	if currentTag == "" {
		return
	}

	diffEngine.CheckWorkloadTag(currentTag, desiredTag, gitOpsEngine, diff)
}

// checkFluxDistributionVersionDrift queries the running FluxInstance for its
// spec.distribution.version and compares it against the version KSail would seed
// (from spec.workload.flux.distributionVersion or a repo-declared FluxInstance).
// If they differ, an in-place change is appended so cluster update re-asserts the
// FluxInstance. Flux only. Errors during cluster queries are logged as warnings
// and skipped — they should not block the rest of the update.
func checkFluxDistributionVersionDrift(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	diffEngine *specdiff.Engine,
	diff *clusterupdate.UpdateResult,
) {
	gitOpsEngine := ctx.ClusterCfg.Spec.Cluster.GitOpsEngine
	if gitOpsEngine != v1alpha1.GitOpsEngineFlux {
		return
	}

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot resolve kubeconfig path for Flux distribution-version drift detection: %v", err)

		return
	}

	instance, err := fluxinstaller.GetCurrentFluxInstance(cmd.Context(), kubeconfigPath)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot query current Flux distribution version for drift detection: %v", err)

		return
	}

	// A missing FluxInstance (or an empty version) means there is nothing to
	// compare against yet — no drift.
	if instance == nil || instance.Spec.Distribution.Version == "" {
		return
	}

	desiredVersion := fluxinstaller.ResolveDesiredDistributionVersion(ctx.ClusterCfg)

	diffEngine.CheckFluxDistributionVersion(
		instance.Spec.Distribution.Version,
		desiredVersion,
		gitOpsEngine,
		diff,
	)
}

// getCurrentArgoCDTargetRevision queries the ArgoCD Application for its current
// targetRevision. Returns empty string if the Application does not exist.
func getCurrentArgoCDTargetRevision(
	goCtx context.Context,
	kubeconfigPath string,
) (string, error) {
	mgr, err := argocdclient.NewManagerFromKubeconfig(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("create argocd manager: %w", err)
	}

	rev, err := mgr.GetCurrentTargetRevision(goCtx, "")
	if err != nil {
		return "", fmt.Errorf("get argocd target revision: %w", err)
	}

	return rev, nil
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

	if !diff.HasInPlaceChanges() && !diff.HasRebootRequired() && !diff.HasRollingRecreate() {
		reportNoApplicableChanges(cmd, diff)

		return nil
	}

	allowRolling, proceed := confirmDisruptiveChanges(cmd, diff, force)
	if !proceed {
		notify.Infof(cmd.OutOrStdout(), "Update cancelled")

		return nil
	}

	reconciler := newComponentReconciler(cmd, ctx.ClusterCfg, clusterName)

	return applyInPlaceChanges(
		cmd, updater, reconciler, clusterName,
		currentSpec, ctx, diff, outputTimer, force, allowRolling,
	)
}

// confirmDisruptiveChanges prompts for confirmation when the diff contains
// disruptive changes (node reboots or rolling node replacement) and --force/--yes
// was not set. It returns whether rolling node replacement is authorized and
// whether the update should proceed.
//
// Rolling replacement is authorized by an explicit --force OR an interactive
// confirmation. It is reported separately from Force (which governs partition
// wipes) so that confirming a rolling replacement never implicitly authorizes a
// wipe that may be discovered during apply and was not shown in the prompt.
func confirmDisruptiveChanges(
	cmd *cobra.Command,
	diff *clusterupdate.UpdateResult,
	force bool,
) (bool, bool) {
	if !diff.HasRebootRequired() && !diff.HasRollingRecreate() {
		return force, true
	}

	if confirm.ShouldSkipPrompt(force) {
		return force, true
	}

	if !promptForDisruptiveChanges(cmd, diff) {
		return false, false
	}

	return force || diff.HasRollingRecreate(), true
}

// promptForDisruptiveChanges warns about reboot-required and rolling-recreate
// changes and prompts the user to confirm. It returns true when the user
// consents to proceed.
func promptForDisruptiveChanges(cmd *cobra.Command, diff *clusterupdate.UpdateResult) bool {
	var block strings.Builder

	if diff.HasRollingRecreate() {
		fmt.Fprintf(
			&block,
			"%d change(s) require rolling node replacement (one node at a time):\n",
			len(diff.RollingRecreate),
		)

		for _, change := range diff.RollingRecreate {
			fmt.Fprintf(
				&block, "  ⚠ %s: %s → %s. %s\n",
				change.Field, change.OldValue, change.NewValue, change.Reason,
			)
		}
	}

	if diff.HasRebootRequired() {
		fmt.Fprintf(&block, "%d change(s) require node reboots:\n", len(diff.RebootRequired))

		for _, change := range diff.RebootRequired {
			fmt.Fprintf(
				&block, "  ⚠ %s: %s → %s. %s\n",
				change.Field, change.OldValue, change.NewValue, change.Reason,
			)
		}
	}

	notify.Warningf(cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

	_, _ = fmt.Fprintf(
		cmd.OutOrStdout(),
		"Type \"yes\" to proceed with these changes: ",
	)

	return confirm.PromptForConfirmation(cmd.OutOrStdout())
}

// reportNoApplicableChanges prints the appropriate message when there are no
// changes to apply. It distinguishes a genuinely clean cluster from one whose
// current state could not be read (unknown baseline), so the latter is not
// reported as "No changes detected". Any unknown-baseline table is rendered by
// the caller via displayChangesSummary before this is invoked.
func reportNoApplicableChanges(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	if diff != nil && diff.HasUnknownBaseline() {
		notify.Warningf(
			cmd.ErrOrStderr(),
			"Current cluster state could not be read for %d component(s); shown as "+
				"Unknown. No changes applied.",
			len(diff.UnknownBaseline),
		)

		return
	}

	notify.Infof(cmd.OutOrStdout(), "No changes detected")
}

// reportDryRun prints a summary for dry-run mode and confirms no changes were applied.
// When --output json is set, emits machine-readable JSON only for the empty-diff case
// (displayChangesSummary already emits JSON when there is anything to report).
func reportDryRun(cmd *cobra.Command, diff *clusterupdate.UpdateResult) error {
	if getOutputFormat(cmd) == outputFormatJSON {
		// displayChangesSummary already emitted JSON when there were changes or an
		// unknown baseline. Only emit JSON here for the genuinely empty case so
		// CI/MCP still get a result.
		if diff != nil && diff.TotalChanges() == 0 && !diff.HasUnknownBaseline() {
			emitDiffJSON(cmd, diff)
		}

		return nil
	}

	if diff != nil && diff.TotalChanges() == 0 {
		reportNoApplicableChanges(cmd, diff)

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
		fmt.Fprintf(
			&block, "  ✗ %s: cannot change from %s to %s in-place. %s\n",
			change.Field, change.OldValue, change.NewValue, change.Reason,
		)
	}

	notify.Warningf(cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

	return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
}

// errUpdateChangesFailed signals that one or more changes failed to apply during
// an in-place cluster update. reportFailedChanges has already printed the
// per-change details; this sentinel exists so cobra surfaces a non-zero exit
// (issue #4935) instead of reporting success on a partial or fully-failed apply.
var errUpdateChangesFailed = errors.New("one or more changes failed to apply")

// applyInPlaceChanges applies provisioner-level and component-level changes in-place.
// force reflects an explicit --force/--yes (and governs partition wipes), while
// allowRolling carries consent for rolling node replacement (via --force or an
// interactive confirmation); the provisioner gates each separately in PrepareUpdate.
func applyInPlaceChanges(
	cmd *cobra.Command,
	updater clusterprovisioner.Updater,
	reconciler *componentReconciler,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	ctx *localregistry.Context,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
	force bool,
	allowRolling bool,
) error {
	updateOpts := clusterupdate.UpdateOptions{
		DryRun:               false,
		RollingReboot:        true,
		Force:                force,
		AllowRollingRecreate: allowRolling,
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
		notify.SuccessWithTimerf(
			cmd.OutOrStdout(), outputTimer,
			"applied %d changes successfully", len(result.AppliedChanges),
		)
	}

	reportFailedChanges(cmd, result)

	// A non-empty FailedChanges set means the apply was partial or fully failed.
	// Provisioner-level failures (e.g. a rejected Talos config) are recorded in
	// result.FailedChanges with a nil Update error, and reconcileComponents
	// appends component-level failures there too. Return a non-nil error so cobra
	// exits non-zero — otherwise automation gating on the exit code treats a
	// failed update as success (issue #4935). Skip the state save: a partial apply
	// no longer matches the desired spec, so it must not become the saved baseline.
	if result.HasFailedChanges() {
		if componentErr != nil {
			return fmt.Errorf("%w: %w", errUpdateChangesFailed, componentErr)
		}

		return errUpdateChangesFailed
	}

	// Persist the updated ClusterSpec for future update baselines now that every
	// change applied successfully.
	saveErr := state.SaveClusterSpec(clusterName, &ctx.ClusterCfg.Spec.Cluster)
	if saveErr != nil {
		notify.Warningf(cmd.OutOrStderr(), "failed to save cluster state: %v", saveErr)
	}

	return nil
}

// reportFailedChanges prints any failed changes from the update result to stderr.
func reportFailedChanges(cmd *cobra.Command, result *clusterupdate.UpdateResult) {
	if len(result.FailedChanges) == 0 {
		return
	}

	var failBlock strings.Builder

	fmt.Fprintf(&failBlock, "%d changes failed to apply:\n", len(result.FailedChanges))

	for _, change := range result.FailedChanges {
		fmt.Fprintf(&failBlock, "  - %s: %s\n", change.Field, change.Reason)
	}

	notify.Errorf(cmd.OutOrStderr(), strings.TrimRight(failBlock.String(), "\n"))
}

// displayChangesSummary outputs a human-readable summary of configuration changes
// as a before/after table with one row per changed field and impact icons.
// Rows are ordered by severity: recreate-required → rolling-recreate → wipe-required →
// reboot-required → in-place.
// Fields with no change are omitted.
// When --output json is set, emits machine-readable JSON instead of the table.
func displayChangesSummary(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	if diff.TotalChanges() == 0 && !diff.HasUnknownBaseline() {
		return
	}

	if getOutputFormat(cmd) == outputFormatJSON {
		emitDiffJSON(cmd, diff)

		return
	}

	notify.Titlef(cmd.OutOrStdout(), "🔍", "Change summary")

	notify.Infof(
		cmd.OutOrStdout(),
		formatDiffTable(diff),
	)
}

// diffRow holds a single row of the diff table.
type diffRow struct {
	icon   string
	field  string
	oldVal string
	newVal string
	impact string
}

// categoryIcon returns the severity icon for a change category.
func categoryIcon(cat clusterupdate.ChangeCategory) string {
	switch cat {
	case clusterupdate.ChangeCategoryRecreateRequired:
		return "🔴"
	case clusterupdate.ChangeCategoryRollingRecreate:
		return "🟠"
	case clusterupdate.ChangeCategoryWipeRequired:
		return "⚠️"
	case clusterupdate.ChangeCategoryRebootRequired:
		return "🟡"
	case clusterupdate.ChangeCategoryInPlace:
		return "🟢"
	case clusterupdate.ChangeCategoryUnknown:
		return "⚪"
	default:
		return "⚪"
	}
}

// formatDiffTable builds the formatted diff table string.
// The table has four columns: Component, Before, After, Impact.
// Rows are ordered by severity: 🔴 recreate → 🟠 rolling-recreate → ⚠️ wipe →
// 🟡 reboot → 🟢 in-place → ⚪ unknown.
func formatDiffTable(
	diff *clusterupdate.UpdateResult,
) string {
	realChanges := diff.TotalChanges()
	unknownCount := len(diff.UnknownBaseline)
	rows := collectDiffRows(diff, realChanges+unknownCount)

	// Column headers
	const (
		hdrComponent = "Component"
		hdrBefore    = "Before"
		hdrAfter     = "After"
		hdrImpact    = "Impact"
	)

	colW, colB, colA, colI := computeColumnWidths(
		rows, hdrComponent, hdrBefore, hdrAfter, hdrImpact,
	)

	var block strings.Builder

	// Pre-allocate: each row needs ~colW+colB+colA+colI bytes for data,
	// plus ~16 bytes overhead per row for spacing (6), emoji (4), newlines, padding.
	const tableOverheadRows = 4 // summary, header, separator, trailing

	const perRowPadding = 16 // spacing + emoji + newline

	block.Grow((len(rows) + tableOverheadRows) * (colW + colB + colA + colI + perRowPadding))

	writeSummaryLine(&block, realChanges, unknownCount)
	writeHeaderRow(&block, colW, colB, colA, hdrComponent, hdrBefore, hdrAfter, hdrImpact)
	writeSeparatorRow(&block, colW, colB, colA, colI)
	writeDataRows(&block, rows, colW, colB, colA)

	return strings.TrimRight(block.String(), "\n")
}

// appendChangesAsRows converts a slice of Changes into diffRows and appends
// them to rows, returning the extended slice.
func appendChangesAsRows(rows []diffRow, changes []clusterupdate.Change) []diffRow {
	for _, c := range changes {
		rows = append(rows, diffRow{
			categoryIcon(c.Category), c.Field, c.OldValue, c.NewValue, c.Category.String(),
		})
	}

	return rows
}

// collectDiffRows builds an ordered list of diff rows.
// Order: 🔴 recreate-required → 🟠 rolling-recreate → ⚠️ wipe-required →
// 🟡 reboot-required → 🟢 in-place → ⚪ unknown.
func collectDiffRows(
	diff *clusterupdate.UpdateResult,
	totalRows int,
) []diffRow {
	rows := make([]diffRow, 0, totalRows)
	rows = appendChangesAsRows(rows, diff.RecreateRequired)
	rows = appendChangesAsRows(rows, diff.RollingRecreate)
	rows = appendChangesAsRows(rows, diff.WipeRequired)
	rows = appendChangesAsRows(rows, diff.RebootRequired)
	rows = appendChangesAsRows(rows, diff.InPlaceChanges)
	rows = appendChangesAsRows(rows, diff.UnknownBaseline)

	return rows
}

// computeColumnWidths returns the max width for each table column.
func computeColumnWidths(
	rows []diffRow,
	hdrComp, hdrBefore, hdrAfter, hdrImpact string,
) (int, int, int, int) {
	widthComp := len(hdrComp)
	widthBefore := len(hdrBefore)
	widthAfter := len(hdrAfter)
	widthImpact := len(hdrImpact)

	for _, row := range rows {
		if length := len(row.field); length > widthComp {
			widthComp = length
		}

		if length := len(row.oldVal); length > widthBefore {
			widthBefore = length
		}

		if length := len(row.newVal); length > widthAfter {
			widthAfter = length
		}

		if length := len(row.impact); length > widthImpact {
			widthImpact = length
		}
	}

	return widthComp, widthBefore, widthAfter, widthImpact
}

func writeSummaryLine(block *strings.Builder, realChanges, unknownCount int) {
	switch {
	case unknownCount > 0 && realChanges > 0:
		fmt.Fprintf(block,
			"Detected %d configuration change(s); %d component(s) have an unknown "+
				"baseline (current cluster state could not be read):\n\n",
			realChanges, unknownCount)
	case unknownCount > 0:
		fmt.Fprintf(block,
			"Current cluster state could not be read; %d component(s) shown as Unknown:\n\n",
			unknownCount)
	default:
		fmt.Fprintf(block, "Detected %d configuration changes:\n\n", realChanges)
	}
}

// headerIndent is the number of leading spaces in the header and separator rows.
// This visually aligns with the emoji+space prefix in data rows:
// emoji renders as 2 terminal columns + 1 trailing space = 3 visual columns.
const headerIndent = "   "

func writeHeaderRow(
	block *strings.Builder,
	colW, colB, colA int,
	hdrComp, hdrBefore, hdrAfter, hdrImpact string,
) {
	fmt.Fprintf(block, "%s%-*s  %-*s  %-*s  %s\n",
		headerIndent,
		colW, hdrComp, colB, hdrBefore, colA, hdrAfter, hdrImpact)
}

func writeSeparatorRow(
	block *strings.Builder,
	colW, colB, colA, colI int,
) {
	fmt.Fprintf(block, "%s%s  %s  %s  %s\n",
		headerIndent,
		strings.Repeat("─", colW),
		strings.Repeat("─", colB),
		strings.Repeat("─", colA),
		strings.Repeat("─", colI))
}

func writeDataRows(
	block *strings.Builder,
	rows []diffRow,
	colW, colB, colA int,
) {
	for _, r := range rows {
		fmt.Fprintf(block, "%s %-*s  %-*s  %-*s  %s\n",
			r.icon, colW, r.field,
			colB, r.oldVal,
			colA, r.newVal,
			r.impact)
	}
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

	_, _ = fmt.Fprintf(
		cmd.OutOrStdout(),
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
// or if the --yes flag was explicitly set to true on the command line.
// This consolidates the --force/--yes alias logic into one place.
func resolveForce(viperForce bool, yesFlag *pflag.Flag) bool {
	return viperForce || (yesFlag != nil && yesFlag.Changed && yesFlag.Value.String() == "true")
}

// configureOIDCKubeconfig adds OIDC exec credential plugin entries to the kubeconfig
// after cluster creation when OIDC authentication is configured.
func configureOIDCKubeconfig(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
) error {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}

	displayName := lifecycle.ExtractClusterNameFromContext(
		clusterCfg.Spec.Cluster.Connection.Context,
		clusterCfg.Spec.Cluster.Distribution,
	)

	// Resolve the actual cluster entry name from the kubeconfig by looking up
	// the context. This is necessary because the context name and cluster entry
	// name differ for some distributions (e.g. Talos uses context "admin@<name>"
	// but cluster entry "<name>").
	contextName := clusterCfg.Spec.Cluster.Connection.Context

	clusterEntryName, resolveErr := resolveClusterEntryName(kubeconfigPath, contextName)
	if resolveErr != nil {
		// Fall back to using the context name directly (works for Kind, K3d, VCluster)
		clusterEntryName = contextName
	}

	oidc := &clusterCfg.Spec.Cluster.OIDC

	err = k8s.AddOIDCKubeconfigEntries(&k8s.OIDCExecConfig{
		KubeconfigPath:   kubeconfigPath,
		ClusterEntryName: clusterEntryName,
		DisplayName:      displayName,
		IssuerURL:        oidc.IssuerURL,
		ClientID:         oidc.ClientID,
		ExtraScopes:      oidc.ExtraScopes,
		CAFile:           oidc.CAFile,
	}, cmd.OutOrStdout())
	if err != nil {
		return fmt.Errorf("failed to add OIDC kubeconfig entries: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "OIDC context 'oidc@%s' added to kubeconfig (use 'kubectl config use-context oidc@%s' to switch)",
		Args:    []any{displayName, displayName},
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// resolveClusterEntryName reads the kubeconfig and returns the cluster entry
// name that the given context references. This handles distributions where
// the context name differs from the cluster entry name (e.g. Talos).
func resolveClusterEntryName(kubeconfigPath, contextName string) (string, error) {
	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}

	kubeconfigBytes, err := os.ReadFile(canonicalPath) //nolint:gosec // canonicalized above
	if err != nil {
		return "", fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	ctxEntry, ok := kubeConfig.Contexts[contextName]
	if !ok || ctxEntry == nil {
		return "", fmt.Errorf("%w: %s", errContextNotFound, contextName)
	}

	return ctxEntry.Cluster, nil
}

// diffExitCode is the exit code returned by the diff command when --exit-code is
// set and configuration drift is detected. This is a KSail-specific convention:
// 0 = no drift, 1 = error, 2 = drift detected.
// (Note: diff(1) uses 1 for differences; KSail reserves 1 for command errors and
// uses 2 for drift so CI scripts can distinguish drift from failures.)
const diffExitCode = 2
