package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfighook"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	argocdclient "github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
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
)

// updateOrchestrator carries the run state shared across every phase of a
// cluster update (version reconciliation, diff computation, apply, recreate).
// Holding cmd/cfgManager/ctx/deps/clusterName/consent/forceDrain/dryRun on the
// receiver replaces the long positional-argument signatures the phases used to
// thread. consent governs prompt-skipping and rolling-recreate authorization
// (--yes or the deprecated --force); forceDrain governs the destructive
// PDB-bypassing drain and partition wipes (--force-drain).
type updateOrchestrator struct {
	cmd         *cobra.Command
	cfgManager  *ksailconfigmanager.ConfigManager
	ctx         *localregistry.Context
	deps        lifecycle.Deps
	clusterName string
	consent     bool
	forceDrain  bool
	dryRun      bool
}

// newUpdateOrchestrator builds the orchestrator from the resolved run state.
func newUpdateOrchestrator(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	consent bool,
	forceDrain bool,
) *updateOrchestrator {
	return &updateOrchestrator{
		cmd:         cmd,
		cfgManager:  cfgManager,
		ctx:         ctx,
		deps:        deps,
		clusterName: clusterName,
		consent:     consent,
		forceDrain:  forceDrain,
		dryRun:      cfgManager.Viper.GetBool("dry-run"),
	}
}

// run executes the cluster update logic. It computes a diff between current and
// desired configuration, then applies changes in-place where possible, falling
// back to cluster recreation when necessary.
func (o *updateOrchestrator) run(outputTimer timer.Timer) error {
	provisioner, err := createAndVerifyProvisioner(o.cmd, o.ctx, o.clusterName)
	if err != nil {
		return err
	}

	// Fail fast when the user pinned a context the kubeconfig cannot resolve, so
	// update never reports "No changes detected" for a cluster it could not inspect.
	err = ensureConfiguredContextResolvable(o.ctx.ClusterCfg)
	if err != nil {
		return err
	}

	// Reconcile cluster versions declaratively on every update: each dimension
	// follows the latest supported version when unset, or the pinned value when set
	// (spec.cluster.kubernetesVersion / spec.cluster.talos.version, overridable via
	// --kubernetes-version / --distribution-version).
	recreated, err := o.reconcileClusterVersions(provisioner)
	if err != nil {
		return err
	}
	// If the cluster was recreated, skip the regular update flow — recreation
	// already started a fresh cluster at the target version.
	if recreated {
		return nil
	}

	// Check if provisioner supports updates
	updater, supportsUpdate := provisioner.(clusterprovisioner.Updater)
	if !supportsUpdate {
		return o.runWithoutUpdater()
	}

	// Compute full diff; return error if current config cannot be retrieved
	// instead of falling back to recreation, which would be destructive.
	currentSpec, diff, diffErr := o.computeUpdateDiff(updater)
	if diffErr != nil {
		return diffErr
	}

	// Display changes summary
	displayChangesSummary(o.cmd, diff)

	return o.applyOrReportChanges(updater, currentSpec, diff, outputTimer)
}

// runWithoutUpdater handles distributions whose provisioner does not implement
// the Updater interface (e.g. VCluster): it computes a spec-level diff to avoid
// blind recreation when nothing changed, honors dry-run, and otherwise recreates.
func (o *updateOrchestrator) runWithoutUpdater() error {
	specDiff := computeSpecOnlyDiff(o.cmd, o.ctx)
	if specDiff.TotalChanges() == 0 {
		// Surface any unknown-baseline components so the user sees that the
		// current state could not be read, then exit without recreating.
		if specDiff.HasUnknownBaseline() {
			displayChangesSummary(o.cmd, specDiff)
		}

		reportNoApplicableChanges(o.cmd, specDiff)

		return nil
	}

	if o.dryRun {
		displayChangesSummary(o.cmd, specDiff)
		notify.Infof(
			o.cmd.OutOrStdout(),
			"Provisioner does not support in-place updates; "+
				"recreation would be required.\nDry run complete. No changes applied.",
		)

		return nil
	}

	return o.executeRecreateFlow()
}

// reconcileClusterVersions reconciles the cluster's distribution and Kubernetes
// versions toward their declared targets on every update. Each dimension follows
// the latest stable version available in the OCI registry when no version is
// declared (spec.cluster.talos.version / spec.cluster.kubernetesVersion, also
// settable via --distribution-version / --kubernetes-version), or reconciles
// toward the declared value when one is set. Distribution is reconciled first
// (the runtime must support the target Kubernetes version). Returns true when the
// cluster was recreated, in which case the caller skips the regular update flow.
//
// Distributions without an Upgrader (e.g. KWOK, EKS) have no version
// reconciliation; the regular update flow handles their changes.
func (o *updateOrchestrator) reconcileClusterVersions(
	provisioner clusterprovisioner.Provisioner,
) (bool, error) {
	upgrader, ok := provisioner.(clusterupdate.Upgrader)
	if !ok {
		return false, nil
	}

	currentVersions, err := upgrader.GetCurrentVersions(o.cmd.Context(), o.clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to get current versions: %w", err)
	}

	resolver := versionresolver.NewOCIResolver()

	// Distribution version first (the runtime must support the target K8s version).
	recreated, err := o.reconcileDistributionVersion(upgrader, resolver, currentVersions)
	if err != nil {
		return false, err
	}

	if recreated {
		return true, nil
	}

	// Then Kubernetes version. A non-recreating distribution upgrade does not move
	// the running Kubernetes version (Talos upgrades the OS only; Kind/K3d recreate,
	// which returns above), so the versions fetched above are still current.
	return o.reconcileKubernetesVersion(upgrader, resolver, currentVersions)
}

// reconcileDistributionVersion reconciles the distribution (OS) version: it caps
// at spec.cluster.talos.version when that pin is set (Talos), otherwise it follows
// the latest stable version discovered from the OCI registry.
func (o *updateOrchestrator) reconcileDistributionVersion(
	upgrader clusterupdate.Upgrader,
	resolver versionresolver.Resolver,
	currentVersions *clusterupdate.VersionInfo,
) (bool, error) {
	if pin := upgrader.PinnedDistributionVersion(); pin != "" {
		return o.executePinnedUpgrade(
			upgrader, distributionLabel, distributionLabel, upgrader.UpgradeDistribution, pin,
			currentVersions.DistributionVersion,
		)
	}

	return o.executeVersionUpgrade(versionUpgradeParams{
		upgrader:       upgrader,
		resolver:       resolver,
		upgradeType:    distributionLabel,
		imageRef:       upgrader.DistributionImageRef(),
		currentVersion: currentVersions.DistributionVersion,
		suffix:         upgrader.VersionSuffix(),
		applyFn:        upgrader.UpgradeDistribution,
	})
}

// reconcileKubernetesVersion reconciles the Kubernetes version: it targets
// spec.cluster.kubernetesVersion when that pin is set, otherwise it follows the
// latest stable version discovered from the OCI registry.
func (o *updateOrchestrator) reconcileKubernetesVersion(
	upgrader clusterupdate.Upgrader,
	resolver versionresolver.Resolver,
	currentVersions *clusterupdate.VersionInfo,
) (bool, error) {
	// An explicit spec.cluster.kubernetesVersion wins; otherwise the upgrader may
	// pin the Kubernetes version implicitly (VCluster bakes it into the embedded SDK
	// image, so it cannot be reached by a discovery-driven recreation).
	pin := strings.TrimSpace(o.ctx.ClusterCfg.Spec.Cluster.KubernetesVersion)
	if pin == "" {
		pin = strings.TrimSpace(upgrader.PinnedKubernetesVersion())
	}

	if pin != "" {
		return o.executePinnedUpgrade(
			upgrader, "Kubernetes", "Kubernetes", upgrader.UpgradeKubernetes, pin,
			currentVersions.KubernetesVersion,
		)
	}

	return o.executeVersionUpgrade(versionUpgradeParams{
		upgrader:       upgrader,
		resolver:       resolver,
		upgradeType:    "Kubernetes",
		imageRef:       upgrader.KubernetesImageRef(),
		currentVersion: currentVersions.KubernetesVersion,
		suffix:         upgrader.VersionSuffix(),
		applyFn:        upgrader.UpgradeKubernetes,
	})
}

// distributionLabel names the distribution (OS) version dimension in user output
// and as the PrepareConfigForVersion key.
const distributionLabel = "distribution"

// upgradeFunc is the signature for UpgradeKubernetes / UpgradeDistribution.
type upgradeFunc func(ctx context.Context, clusterName, fromVersion, toVersion string) error

// versionUpgradeParams bundles the per-dimension inputs to executeVersionUpgrade
// (which version dimension, its image reference, current version, and apply hook),
// keeping the method receiver free of the long positional argument list.
type versionUpgradeParams struct {
	upgrader       clusterupdate.Upgrader
	resolver       versionresolver.Resolver
	upgradeType    string
	imageRef       string
	currentVersion string
	suffix         string
	applyFn        upgradeFunc
}

// executeVersionUpgrade discovers available versions, computes an upgrade path,
// and applies each step. For distributions requiring recreation, it jumps to the
// latest version and triggers a single recreate.
func (o *updateOrchestrator) executeVersionUpgrade(params versionUpgradeParams) (bool, error) {
	if params.imageRef == "" {
		// This distribution has no separate image to discover versions from for
		// this dimension (e.g. it bundles its version with Kubernetes); nothing to
		// reconcile here.
		return false, nil
	}

	notify.WriteMessage(notify.Message{
		Type: notify.ActivityType,
		Content: fmt.Sprintf(
			"discovering available %s versions from %s", params.upgradeType, params.imageRef,
		),
		Writer: o.cmd.OutOrStdout(),
	})

	path, err := versionresolver.ComputeUpgradePath(
		o.cmd.Context(), params.resolver, params.imageRef, params.currentVersion, params.suffix,
	)
	if err != nil {
		return o.handleUpgradePathError(params.upgradeType, params.currentVersion, err)
	}

	// Display upgrade path
	notify.WriteMessage(notify.Message{
		Type: notify.InfoType,
		Content: fmt.Sprintf(
			"%s upgrade path: %s → %s (%d step(s))",
			params.upgradeType,
			params.currentVersion,
			path[len(path)-1].Version.Original,
			len(path),
		),
		Writer: o.cmd.OutOrStdout(),
	})

	if o.dryRun {
		return o.reportVersionUpgradeDryRun(params.upgradeType, path)
	}

	return o.applyVersionUpgradePath(params, path)
}

// handleUpgradePathError maps a ComputeUpgradePath failure to the right outcome:
// an "already latest" no-op is informational, anything else is surfaced.
func (o *updateOrchestrator) handleUpgradePathError(
	upgradeType, currentVersion string,
	err error,
) (bool, error) {
	if errors.Is(err, versionresolver.ErrNoUpgradesAvailable) {
		notify.Infof(o.cmd.OutOrStdout(),
			"%s is already at the latest stable version (%s)", upgradeType, currentVersion)

		return false, nil
	}

	return false, fmt.Errorf("failed to compute %s upgrade path: %w", upgradeType, err)
}

// reportVersionUpgradeDryRun prints the computed upgrade path without applying it.
func (o *updateOrchestrator) reportVersionUpgradeDryRun(
	upgradeType string,
	path []versionresolver.UpgradeStep,
) (bool, error) {
	for i, step := range path {
		_, _ = fmt.Fprintf(o.cmd.OutOrStdout(), "  %d. %s\n", i+1, step.Version.Original)
	}

	notify.Infof(o.cmd.OutOrStdout(), "Dry run complete. No %s upgrades applied.", upgradeType)

	return false, nil
}

// applyVersionUpgradePath probes the first upgrade step to discover the upgrade
// mechanism: recreation-based distributions (Kind/K3d/VCluster) return
// ErrRecreationRequired without touching the cluster, so the path jumps to the
// latest version and recreates once; rolling-upgrade distributions (Talos) apply
// the first step in the probe and continue with the remaining steps.
func (o *updateOrchestrator) applyVersionUpgradePath(
	params versionUpgradeParams,
	path []versionresolver.UpgradeStep,
) (bool, error) {
	targetVersion := path[len(path)-1].Version.Original

	probeErr := params.applyFn(
		o.cmd.Context(), o.clusterName, params.currentVersion, path[0].Version.Original,
	)
	if probeErr != nil && errors.Is(probeErr, clustererr.ErrUpgradeSkipped) {
		notify.Infof(o.cmd.OutOrStdout(), "%s upgrade skipped: %v", params.upgradeType, probeErr)

		return false, nil
	}

	if probeErr != nil && errors.Is(probeErr, clustererr.ErrRecreationRequired) {
		prepErr := params.upgrader.PrepareConfigForVersion(params.upgradeType, targetVersion)
		if prepErr != nil {
			return false, fmt.Errorf(
				"failed to prepare config for %s %s: %w",
				params.upgradeType, targetVersion, prepErr,
			)
		}

		return true, o.handleRecreationUpgrade(
			params.upgradeType, params.currentVersion, targetVersion,
		)
	}

	// Rolling upgrade (Talos): first step already applied by the probe, continue with the rest.
	if probeErr != nil {
		return false, fmt.Errorf(
			"%s upgrade failed at step 1/%d (%s → %s), cluster is still running %s: %w",
			params.upgradeType, len(path), params.currentVersion, path[0].Version.Original,
			params.currentVersion, probeErr,
		)
	}

	notify.WriteMessage(notify.Message{
		Type: notify.SuccessType,
		Content: fmt.Sprintf("%s upgraded: step 1/%d → %s",
			params.upgradeType, len(path), path[0].Version.Original),
		Writer: o.cmd.OutOrStdout(),
	})

	return o.applyRemainingUpgradeSteps(params, path, targetVersion)
}

// applyRemainingUpgradeSteps applies the rolling-upgrade steps after the probe
// step, reporting per-step progress and the final completion message.
func (o *updateOrchestrator) applyRemainingUpgradeSteps(
	params versionUpgradeParams,
	path []versionresolver.UpgradeStep,
	targetVersion string,
) (bool, error) {
	for stepIdx := 1; stepIdx < len(path); stepIdx++ {
		step := path[stepIdx]
		prevVersion := path[stepIdx-1].Version.Original

		notify.WriteMessage(notify.Message{
			Type: notify.ActivityType,
			Content: fmt.Sprintf("upgrading %s: step %d/%d (%s → %s)",
				params.upgradeType, stepIdx+1, len(path), prevVersion, step.Version.Original),
			Writer: o.cmd.OutOrStdout(),
		})

		applyErr := params.applyFn(
			o.cmd.Context(), o.clusterName, prevVersion, step.Version.Original,
		)
		if applyErr != nil {
			notify.Warningf(o.cmd.OutOrStderr(),
				"%s upgrade to %s failed (cluster is at %s): %v",
				params.upgradeType, step.Version.Original, prevVersion, applyErr)

			return false, fmt.Errorf(
				"%s upgrade failed at step %d/%d (%s → %s), cluster is running %s: %w",
				params.upgradeType, stepIdx+1, len(path), prevVersion, step.Version.Original,
				prevVersion, applyErr,
			)
		}

		notify.WriteMessage(notify.Message{
			Type: notify.SuccessType,
			Content: fmt.Sprintf("%s upgraded: step %d/%d → %s",
				params.upgradeType, stepIdx+1, len(path), step.Version.Original),
			Writer: o.cmd.OutOrStdout(),
		})
	}

	notify.WriteMessage(notify.Message{
		Type: notify.SuccessType,
		Content: fmt.Sprintf(
			"%s upgrade complete: %s → %s",
			params.upgradeType, params.currentVersion, targetVersion,
		),
		Writer: o.cmd.OutOrStdout(),
	})

	return false, nil
}

// handleRecreationUpgrade handles version upgrades for distributions that require
// cluster recreation (Kind, K3d, VCluster). It confirms with the user, then
// recreates the cluster which will pick up the latest configured version.
func (o *updateOrchestrator) handleRecreationUpgrade(
	upgradeType string,
	currentVersion, targetVersion string,
) error {
	notify.WriteMessage(notify.Message{
		Type: notify.InfoType,
		Content: fmt.Sprintf(
			"%s upgrade from %s to %s requires cluster recreation",
			upgradeType, currentVersion, targetVersion,
		),
		Writer: o.cmd.OutOrStdout(),
	})

	return o.executeRecreateFlow()
}

// pinnedVersionSkipReason indicates why a pinned version upgrade was skipped.
type pinnedVersionSkipReason int

const (
	pinnedVersionProceed     pinnedVersionSkipReason = iota // No skip — proceed with upgrade.
	pinnedVersionAlreadyAtIt                                // Cluster is already at the pinned version.
	pinnedVersionNewer                                      // Cluster is newer than the pinned version.
)

// reportPinnedUpgradePreamble normalizes a pinned version for the given dimension
// (label, e.g. "distribution" or "Kubernetes") and reports the no-op cases:
// already at the pin, newer than the pin (downgrade guard), or dry-run. It returns
// the normalized version and whether the caller should proceed with the upgrade.
func (o *updateOrchestrator) reportPinnedUpgradePreamble(
	label, rawPinnedVersion, currentVersion string,
) (string, bool, error) {
	pinnedVersion, skipReason, err := normalizePinnedVersion(rawPinnedVersion, currentVersion)
	if err != nil {
		return "", false, err
	}

	switch skipReason {
	case pinnedVersionAlreadyAtIt:
		// Already at the pin is a no-op for this dimension; stay silent so the run
		// only reports diffs, applied changes, and skip/success/fail outcomes. The
		// overall "No changes detected" line already covers this case.
		return pinnedVersion, false, nil
	case pinnedVersionNewer:
		notify.Infof(o.cmd.OutOrStdout(),
			"cluster is at %s which is newer than pinned version %s; skipping downgrade",
			currentVersion, pinnedVersion)

		return pinnedVersion, false, nil
	case pinnedVersionProceed:
	}

	notify.WriteMessage(notify.Message{
		Type: notify.InfoType,
		Content: fmt.Sprintf("%s upgrade capped at pinned version %s (current: %s)",
			label, pinnedVersion, currentVersion),
		Writer: o.cmd.OutOrStdout(),
	})

	if o.dryRun {
		notify.Infof(o.cmd.OutOrStdout(),
			"Dry run complete. Would upgrade %s to pinned version %s.", label, pinnedVersion)

		return pinnedVersion, false, nil
	}

	return pinnedVersion, true, nil
}

// executePinnedUpgrade reconciles one version dimension (distribution or
// Kubernetes) toward an explicit pin, guarding against downgrades and honoring
// dry-run via reportPinnedUpgradePreamble. label names the dimension in user
// output, dimension is the PrepareConfigForVersion key, and applyFn is the
// upgrader method for the dimension. The bool return reports whether the
// cluster was recreated (distributions that cannot upgrade in place — e.g.
// Docker-provider Talos OS upgrades or Kind/K3d/VCluster Kubernetes upgrades —
// reach the pinned version by recreation).
func (o *updateOrchestrator) executePinnedUpgrade(
	upgrader clusterupdate.Upgrader,
	label, dimension string,
	applyFn upgradeFunc,
	rawPinnedVersion string,
	currentVersion string,
) (bool, error) {
	pinnedVersion, proceed, err := o.reportPinnedUpgradePreamble(
		label, rawPinnedVersion, currentVersion,
	)
	if err != nil || !proceed {
		return false, err
	}

	upgradeErr := applyFn(o.cmd.Context(), o.clusterName, currentVersion, pinnedVersion)
	if upgradeErr != nil {
		return o.handlePinnedUpgradeError(
			upgrader, label, dimension, pinnedVersion, currentVersion, upgradeErr,
		)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: label + " upgraded to pinned version " + pinnedVersion,
		Writer:  o.cmd.OutOrStdout(),
	})

	return false, nil
}

// handlePinnedUpgradeError maps an upgrade error for one version dimension to
// the right outcome: a skipped upgrade is informational, a recreation-required
// result prepares the config at the pinned version and recreates the cluster
// (recreation-based distributions reach the pinned version by recreating at
// it — e.g. Docker-provider Talos for the OS, Kind/K3d/VCluster for
// Kubernetes), and any other error is surfaced.
func (o *updateOrchestrator) handlePinnedUpgradeError(
	upgrader clusterupdate.Upgrader,
	label, dimension, pinnedVersion, currentVersion string,
	upgradeErr error,
) (bool, error) {
	if errors.Is(upgradeErr, clustererr.ErrUpgradeSkipped) {
		notify.Infof(o.cmd.OutOrStdout(), "%s upgrade skipped: %v", label, upgradeErr)

		return false, nil
	}

	if errors.Is(upgradeErr, clustererr.ErrRecreationRequired) {
		prepErr := upgrader.PrepareConfigForVersion(dimension, pinnedVersion)
		if prepErr != nil {
			return false, fmt.Errorf(
				"failed to prepare config for %s %s: %w", dimension, pinnedVersion, prepErr,
			)
		}

		return true, o.handleRecreationUpgrade(dimension, currentVersion, pinnedVersion)
	}

	// Error strings start lowercase by convention, so the label is lowered
	// ("Kubernetes" → "kubernetes"); the pre-consolidation messages did the same.
	return false, fmt.Errorf("%s upgrade to pinned version %s failed: %w",
		strings.ToLower(label), pinnedVersion, upgradeErr)
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

	// Already at the pin is a no-op. Compare parsed semver, not raw strings, so a
	// v-prefix mismatch does not hide the no-op — e.g. a distribution whose pin is
	// the unprefixed SDK version "0.34.1" against a current "0.34.1" (VCluster's
	// ChartVersion()) must still resolve to "already at it" rather than triggering a
	// phantom upgrade.
	current, curErr := versionresolver.ParseVersion(currentVersion)
	if currentVersion == pinnedVersion || (curErr == nil && pinned.Equal(current)) {
		return pinnedVersion, pinnedVersionAlreadyAtIt, nil
	}

	// Guard against downgrades: skip if the cluster is already newer than the pin.
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
	factory := newProvisionerFactory(ctx)

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

	// EKS contexts are identity- and region-qualified. When the config leaves the
	// context implicit, bind the exact context written by eksctl before building
	// Helm or Kubernetes clients; an empty Helm context would otherwise use the
	// kubeconfig's unrelated ambient current-context.
	if ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS &&
		strings.TrimSpace(ctx.ClusterCfg.Spec.Cluster.Connection.Context) == "" {
		err = resolveEKSPostCreateContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve exact EKS kubeconfig context: %w", err)
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

// ensureConfiguredContextResolvable fails the update when the user pinned a kube
// context in spec.cluster.connection.context that does not exist in the resolved
// kubeconfig. Without this guard an unresolvable pinned context lets the GitOps
// drift probes fail silently (they only warn) while the command still reports
// "No changes detected" — falsely reassuring, because KSail could not inspect the
// cluster it was told to manage. When no context is pinned, KSail derives one and
// this is a no-op.
//
// Call this only after the kubeconfig has been refreshed (createAndVerifyProvisioner),
// so remote providers (e.g. Omni) that materialise the context on demand are not
// rejected prematurely.
func ensureConfiguredContextResolvable(clusterCfg *v1alpha1.Cluster) error {
	contextName := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context)
	if contextName == "" {
		return nil
	}

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	err = k8s.ValidateContextExists(kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("configured spec.cluster.connection.context is not usable: %w", err)
	}

	return nil
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

// buildComponentDetector builds a ComponentDetector from the cluster's resolved
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

	k8sClientset, err := k8s.NewClientset(kubeconfig, resolveKubeContext(ctx))
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot create K8s clientset for component detection, using defaults: %v", err)

		return nil
	}

	// Docker client is optional — only needed for cloud-provider-kind detection.
	dockerClient, _ := docker.GetDockerClient()

	return detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
}

// resolveKubeContext returns the kube context KSail should use to query the
// cluster it manages: the pinned spec.cluster.connection.context when set,
// otherwise the context name derived from the distribution and cluster name.
// The component detector and the GitOps drift probes share this resolution so
// they all target the configured cluster rather than the ambient
// current-context (which may point elsewhere).
func resolveKubeContext(ctx *localregistry.Context) string {
	// Trim to match ensureConfiguredContextResolvable: a whitespace-padded pinned
	// context must resolve to the same value the guard validated, otherwise it
	// would pass the guard yet break the REST clients the probes build.
	k8sContext := strings.TrimSpace(ctx.ClusterCfg.Spec.Cluster.Connection.Context)
	if k8sContext == "" {
		clusterName := resolveClusterNameFromContext(ctx)
		k8sContext = ctx.ClusterCfg.Spec.Cluster.Distribution.ContextName(clusterName)
	}

	return k8sContext
}

// computeUpdateDiff retrieves current config and computes the full diff.
// Returns an error if current config or the provisioner diff could not be
// retrieved; the caller should surface the error rather than silently
// recreating the cluster or reporting no changes.
func (o *updateOrchestrator) computeUpdateDiff(
	updater clusterprovisioner.Updater,
) (*v1alpha1.ClusterSpec, *clusterupdate.UpdateResult, error) {
	currentSpec, currentProvider, err := updater.GetCurrentConfig(o.cmd.Context(), o.clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"could not retrieve current cluster configuration: %w", err,
		)
	}

	eksRegion := ""
	if o.ctx.EKSConfig != nil {
		eksRegion = o.ctx.EKSConfig.Region
	}

	err = overlayOwnedEKSControllerCleanupBaseline(
		currentSpec,
		&o.ctx.ClusterCfg.Spec.Cluster,
		o.clusterName,
		eksRegion,
	)
	if err != nil {
		return nil, nil, err
	}

	diffEngine := specdiff.NewEngine(
		o.ctx.ClusterCfg.Spec.Cluster.Distribution,
		o.ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	diff := diffEngine.ComputeDiff(
		currentSpec, &o.ctx.ClusterCfg.Spec.Cluster,
		currentProvider, &o.ctx.ClusterCfg.Spec.Provider,
	)

	provisionerDiff, diffErr := updater.DiffConfig(
		o.cmd.Context(), o.clusterName, currentSpec, &o.ctx.ClusterCfg.Spec.Cluster,
	)
	if diffErr != nil {
		return nil, nil, fmt.Errorf("could not compute provisioner configuration diff: %w", diffErr)
	}

	specdiff.MergeProvisionerDiff(diff, provisionerDiff)

	// Check for workload tag drift (stale GitOps sync ref)
	checkWorkloadTagDrift(o.cmd, o.ctx, diffEngine, diff)

	// Check for Flux distribution-version drift (spec.workload.flux.distributionVersion)
	checkFluxDistributionVersionDrift(o.cmd, o.ctx, diffEngine, diff)

	promoteUnsupportedInPlaceChanges(updater, diff)

	return currentSpec, diff, nil
}

func promoteUnsupportedInPlaceChanges(
	updater clusterprovisioner.Updater,
	diff *clusterupdate.UpdateResult,
) {
	fieldSupport, declaresSupport := updater.(clusterprovisioner.InPlaceFieldSupport)
	if !declaresSupport || diff == nil {
		return
	}

	supported := make([]clusterupdate.Change, 0, len(diff.InPlaceChanges))
	for _, change := range diff.InPlaceChanges {
		if isComponentReconcileChange(change) ||
			fieldSupport.SupportsInPlaceField(change.Field) {
			supported = append(supported, change)

			continue
		}

		change.Category = clusterupdate.ChangeCategoryRecreateRequired
		change.Reason = "the selected provisioner cannot apply this field in-place"
		diff.RecreateRequired = append(diff.RecreateRequired, change)
	}

	diff.InPlaceChanges = supported
}

func isComponentReconcileChange(change clusterupdate.Change) bool {
	if change.Field == "cluster.metricsServer" &&
		v1alpha1.MetricsServer(change.NewValue) == v1alpha1.MetricsServerDisabled {
		return false
	}

	return isComponentReconcileField(change.Field)
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
	applyDetectedBaseline(cmd, ctx, currentSpec)

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

// applyDetectedBaseline fills currentSpec with the live cluster's detected
// component state when a detector can be built, otherwise marks the
// detector-derived fields Unknown so the diff does not fabricate a confident
// comparison against default values.
func applyDetectedBaseline(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	currentSpec *v1alpha1.ClusterSpec,
) {
	componentDetector := buildComponentDetector(cmd, ctx)
	if componentDetector == nil {
		// The detector's clients (Helm/K8s) could not be constructed, so live
		// cluster state cannot be read at all. Treat this like a detection
		// failure and mark the baseline Unknown rather than fabricating a
		// confident diff against defaults. buildComponentDetector already logged
		// the underlying reason.
		clusterupdate.MarkComponentsUnknown(currentSpec)

		return
	}

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

		return
	}

	clusterupdate.ApplyDetectedComponents(currentSpec, detected)
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
	if gitOpsEngine.IsNone() {
		return
	}

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot resolve kubeconfig path for workload tag drift detection: %v", err)

		return
	}

	desiredTag := fluxinstaller.ResolveDesiredTag(ctx.ClusterCfg)
	kubeContext := resolveKubeContext(ctx)

	var currentTag string

	switch gitOpsEngine { //nolint:exhaustive // None/empty already filtered above
	case v1alpha1.GitOpsEngineFlux:
		currentTag, err = fluxinstaller.GetCurrentSyncRef(
			cmd.Context(), kubeconfigPath, kubeContext,
		)
	case v1alpha1.GitOpsEngineArgoCD:
		currentTag, err = getCurrentArgoCDTargetRevision(
			cmd.Context(), kubeconfigPath, kubeContext,
		)
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

	instance, err := fluxinstaller.GetCurrentFluxInstance(
		cmd.Context(), kubeconfigPath, resolveKubeContext(ctx),
	)
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
	kubeconfigPath, kubeContext string,
) (string, error) {
	mgr, err := argocdclient.NewManagerFromKubeconfig(kubeconfigPath, kubeContext)
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
func (o *updateOrchestrator) applyOrReportChanges(
	updater clusterprovisioner.Updater,
	currentSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
) error {
	if o.dryRun {
		return reportDryRun(o.cmd, diff)
	}

	if diff.HasRecreateRequired() {
		return o.handleRecreateRequired(diff)
	}

	if !diff.HasInPlaceChanges() && !diff.HasRebootRequired() && !diff.HasRollingRecreate() {
		reportNoApplicableChanges(o.cmd, diff)

		return nil
	}

	allowRolling, proceed := confirmDisruptiveChanges(o.cmd, diff, o.consent)
	if !proceed {
		notify.Infof(o.cmd.OutOrStdout(), "Update cancelled")

		return nil
	}

	eksRegion := ""
	if o.ctx.EKSConfig != nil {
		eksRegion = o.ctx.EKSConfig.Region
	}

	reconciler := newComponentReconciler(
		o.cmd, o.ctx.ClusterCfg, o.clusterName, eksRegion,
	)

	return applyInPlaceChanges(
		o.cmd, updater, reconciler, o.clusterName,
		currentSpec, o.ctx, diff, outputTimer, o.forceDrain, allowRolling,
	)
}

// handleRecreateRequired warns about recreate-required changes and proceeds
// with recreation, prompting for confirmation unless --force is set.
func (o *updateOrchestrator) handleRecreateRequired(diff *clusterupdate.UpdateResult) error {
	var block strings.Builder

	fmt.Fprintf(&block, "%d changes require cluster recreation:\n", len(diff.RecreateRequired))

	for _, change := range diff.RecreateRequired {
		fmt.Fprintf(
			&block, "  ✗ %s: cannot change from %s to %s in-place. %s\n",
			change.Field, change.OldValue, change.NewValue, change.Reason,
		)
	}

	notify.Warningf(o.cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

	return o.executeRecreateFlow()
}

// errUpdateChangesFailed signals that one or more changes failed to apply during
// an in-place cluster update. reportFailedChanges has already printed the
// per-change details; this sentinel exists so cobra surfaces a non-zero exit
// (issue #4935) instead of reporting success on a partial or fully-failed apply.
var errUpdateChangesFailed = errors.New("one or more changes failed to apply")

// applyInPlaceChanges applies provisioner-level and component-level changes in-place.
// forceDrain reflects an explicit --force-drain (and governs partition wipes and
// PDB-bypassing drains), while allowRolling carries consent for rolling node
// replacement (via --yes/--force or an interactive confirmation); the provisioner
// gates each separately in PrepareUpdate.
func applyInPlaceChanges(
	cmd *cobra.Command,
	updater clusterprovisioner.Updater,
	reconciler *componentReconciler,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	ctx *localregistry.Context,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
	forceDrain bool,
	allowRolling bool,
) error {
	err := lifecycle.VerifyAWSOwnershipBeforeMutation(
		cmd.Context(),
		ctx.AWSOwnershipVerifier,
	)
	if err != nil {
		return fmt.Errorf("reverify EKS ownership before update apply: %w", err)
	}

	updateOpts := clusterupdate.UpdateOptions{
		DryRun:               false,
		RollingReboot:        true,
		Force:                forceDrain,
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

	err = lifecycle.VerifyAWSOwnershipBeforeMutation(
		cmd.Context(),
		ctx.AWSOwnershipVerifier,
	)
	if err != nil {
		return fmt.Errorf("reverify EKS ownership before component reconciliation: %w", err)
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

	return finalizeInPlaceApply(cmd, ctx, reconciler, clusterName, result, componentErr)
}

// finalizeInPlaceApply reports the apply outcome: a non-empty FailedChanges set
// means the apply was partial or fully failed, so it returns a non-nil error so
// cobra exits non-zero (issue #4935) and skips the desired ClusterSpec save. An
// EKS controller mutation that already succeeded still persists its narrower
// ownership state. Otherwise it persists the updated spec as the next baseline.
func finalizeInPlaceApply(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	reconciler *componentReconciler,
	clusterName string,
	result *clusterupdate.UpdateResult,
	componentErr error,
) error {
	var (
		componentStateErr       error
		componentStatePersisted bool
	)

	// Component reconciliation can continue after an unrelated failure. Persist
	// a controller mutation that already succeeded before reporting the partial
	// apply, otherwise a later disable loses the only safe ownership evidence.
	if reconciler.hasEKSLoadBalancerOwnershipUpdate() {
		componentStateErr = persistReconciledEKSComponentState(ctx, clusterName, reconciler)
		componentStatePersisted = componentStateErr == nil
	}

	// A non-empty FailedChanges set means the apply was partial or fully failed.
	// Provisioner-level failures (e.g. a rejected Talos config) are recorded in
	// result.FailedChanges with a nil Update error, and reconcileComponents
	// appends component-level failures there too. Return a non-nil error so cobra
	// exits non-zero — otherwise automation gating on the exit code treats a
	// failed update as success (issue #4935). Skip the desired ClusterSpec save: a
	// partial apply no longer matches the desired spec, so it must not become the
	// saved baseline.
	if result.HasFailedChanges() {
		return errors.Join(errUpdateChangesFailed, componentErr, componentStateErr)
	}

	if componentStateErr != nil {
		return componentStateErr
	}

	// Persist the updated ClusterSpec for future update baselines now that every
	// change applied successfully.
	if !componentStatePersisted {
		err := persistReconciledEKSComponentState(ctx, clusterName, reconciler)
		if err != nil {
			return err
		}
	}

	saveErr := state.SaveClusterSpec(clusterName, &ctx.ClusterCfg.Spec.Cluster)
	if saveErr != nil {
		notify.Warningf(cmd.OutOrStderr(), "failed to save cluster state: %v", saveErr)
	}

	return nil
}

// executeRecreateFlow performs the delete + create flow with confirmation.
func (o *updateOrchestrator) executeRecreateFlow() error {
	outputTimer := flags.MaybeTimer(o.cmd, o.deps.Timer)

	if !confirmRecreate(o.cmd, o.clusterName, o.consent) {
		return nil
	}

	err := lifecycle.VerifyAWSOwnershipBeforeMutation(
		o.cmd.Context(),
		o.ctx.AWSOwnershipVerifier,
	)
	if err != nil {
		return fmt.Errorf("reverify EKS ownership before cluster recreation: %w", err)
	}

	// Create provisioner for delete
	factory := newProvisionerFactory(o.ctx)

	provisioner, _, err := factory.Create(o.cmd.Context(), o.ctx.ClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Disconnect registries from Docker network before deletion.
	// Required for distributions like VCluster and Talos because their provisioners
	// destroy the Docker network during deletion, which fails if containers are
	// still connected. Registries are reused on recreate, so only disconnect is needed.
	if o.ctx.ClusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderDocker {
		clusterInfo := &clusterdetector.Info{
			Distribution: o.ctx.ClusterCfg.Spec.Cluster.Distribution,
			ClusterName:  o.clusterName,
		}
		// Discover first: the disconnect is scoped to THIS cluster's registries, because a
		// network name does not identify a single cluster (see DisconnectRegistriesByInfo).
		disconnectRegistriesBeforeDelete(
			o.cmd,
			clusterInfo,
			discoverRegistriesBeforeDelete(o.cmd, clusterInfo),
		)
	}

	// Execute delete
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "deleting existing cluster",
		Writer:  o.cmd.OutOrStdout(),
	})

	err = provisioner.Delete(o.cmd.Context(), o.clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete existing cluster: %w", err)
	}

	err = clearDeletedEKSState(o.ctx, o.clusterName)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster deleted",
		Timer:   outputTimer,
		Writer:  o.cmd.OutOrStdout(),
	})

	// Execute create using shared workflow.
	controllerReconciliationStarted, creationErr := runClusterCreationWorkflow(
		o.cmd,
		o.cfgManager,
		o.ctx,
		o.deps,
	)

	return finishRecreateFlow(
		o.cmd.Context(),
		o.ctx,
		o.clusterName,
		creationErr,
		controllerReconciliationStarted,
	)
}

// finishRecreateFlow keeps successful recreation finalization separate from the
// destructive delete/create orchestration so its required state is testable.
func finishRecreateFlow(
	goCtx context.Context,
	ctx *localregistry.Context,
	clusterName string,
	creationErr error,
	controllerReconciliationStarted bool,
) error {
	err := persistCreatedEKSComponentStateAfterWorkflow(
		goCtx,
		ctx,
		clusterName,
		creationErr,
		controllerReconciliationStarted,
	)
	if err != nil {
		return err
	}

	err = state.SaveClusterSpec(clusterName, &ctx.ClusterCfg.Spec.Cluster)
	if err != nil {
		return fmt.Errorf("persist recreated cluster state: %w", err)
	}

	return nil
}
