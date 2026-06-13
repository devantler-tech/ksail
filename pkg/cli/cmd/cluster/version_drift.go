package cluster

import (
	"errors"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/spf13/cobra"
)

// versionDimension describes one read-only version-reconciliation dimension
// (distribution or Kubernetes) for drift computation.
type versionDimension struct {
	// label names the dimension in change output (e.g. "distribution", "Kubernetes").
	label string
	// imageRef is the OCI repository used to discover the latest stable version.
	// Empty means this dimension has no separately-discoverable version.
	imageRef string
	// currentVersion is the running version of the dimension.
	currentVersion string
	// suffix is the distribution tag suffix (e.g. "k3s") used during discovery.
	suffix string
	// pinnedVersion is the explicitly pinned target, or "" to follow latest.
	pinnedVersion string
	// rolling reports whether reaching the target rolls nodes (Talos OS upgrade)
	// rather than recreating; it selects the change impact category.
	rolling bool
}

// mergeVersionDrift computes read-only version-reconciliation drift — the same
// distribution/Kubernetes version dimension `cluster update` reconciles on every
// run — and merges it into the diff result. For each dimension it resolves the
// target version (the pinned value when set, otherwise the latest stable version
// discovered from the OCI registry) and compares it against the running version,
// never touching the cluster.
//
// This is opt-in (the diff command gates it behind --include-version-drift)
// because resolving "latest" performs OCI registry lookups, which would otherwise
// make `cluster diff` network-dependent and fire its --exit-code CI gate whenever
// upstream cuts a release.
func mergeVersionDrift(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	mainDiff *clusterupdate.UpdateResult,
) {
	upgrader, current, ok := loadVersionUpgrader(cmd, ctx)
	if !ok {
		return
	}

	resolver := versionresolver.NewOCIResolver()

	for _, dimension := range versionDimensions(ctx, upgrader, current) {
		mergeDimensionDrift(cmd, mainDiff, resolver, dimension)
	}
}

// loadVersionUpgrader creates a provisioner for the configured cluster and, when
// it supports version upgrades, returns it together with the cluster's current
// versions. Any failure is surfaced as a non-fatal warning (returning ok=false)
// so version drift never blocks the spec-level diff.
func loadVersionUpgrader(
	cmd *cobra.Command,
	ctx *localregistry.Context,
) (clusterupdate.Upgrader, *clusterupdate.VersionInfo, bool) {
	factory := newProvisionerFactory(ctx)

	provisioner, _, err := factory.Create(cmd.Context(), ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(cmd.ErrOrStderr(),
			"Cannot create provisioner for version-drift detection: %v", err)

		return nil, nil, false
	}

	upgrader, ok := provisioner.(clusterupdate.Upgrader)
	if !ok {
		// Distributions without an Upgrader (e.g. KWOK, EKS) have no version
		// reconciliation; nothing to report.
		return nil, nil, false
	}

	clusterName := resolveClusterNameFromContext(ctx)

	current, err := upgrader.GetCurrentVersions(cmd.Context(), clusterName)
	if err != nil {
		notify.Warningf(cmd.ErrOrStderr(),
			"Cannot retrieve current versions for version-drift detection: %v", err)

		return nil, nil, false
	}

	return upgrader, current, true
}

// versionDimensions returns the distribution and Kubernetes dimensions to check.
func versionDimensions(
	ctx *localregistry.Context,
	upgrader clusterupdate.Upgrader,
	current *clusterupdate.VersionInfo,
) []versionDimension {
	kubePin := strings.TrimSpace(ctx.ClusterCfg.Spec.Cluster.KubernetesVersion)
	if kubePin == "" {
		kubePin = strings.TrimSpace(upgrader.PinnedKubernetesVersion())
	}

	return []versionDimension{
		{
			label:          distributionLabel,
			imageRef:       upgrader.DistributionImageRef(),
			currentVersion: current.DistributionVersion,
			suffix:         upgrader.VersionSuffix(),
			pinnedVersion:  upgrader.PinnedDistributionVersion(),
			// A distinct distribution image ref means a separately-versioned OS
			// (Talos) that upgrades by rolling reboot; otherwise the distribution
			// version equals Kubernetes and only recreation moves it.
			rolling: upgrader.DistributionImageRef() != "",
		},
		{
			label:          "Kubernetes",
			imageRef:       upgrader.KubernetesImageRef(),
			currentVersion: current.KubernetesVersion,
			suffix:         upgrader.VersionSuffix(),
			pinnedVersion:  kubePin,
			rolling:        false,
		},
	}
}

// mergeDimensionDrift resolves one dimension's target version and, when the
// cluster is behind it, appends a drift change to the diff result.
func mergeDimensionDrift(
	cmd *cobra.Command,
	mainDiff *clusterupdate.UpdateResult,
	resolver versionresolver.Resolver,
	dimension versionDimension,
) {
	target, reason, ok := resolveDimensionTarget(cmd, resolver, dimension)
	if !ok {
		return
	}

	if versionsEqual(dimension.currentVersion, target) ||
		isDowngrade(dimension.currentVersion, target) {
		return
	}

	appendVersionChange(mainDiff, dimension, target, reason)
}

// resolveDimensionTarget returns the target version for a dimension: the pinned
// value when set (no network access), otherwise the latest stable version
// discovered from the OCI registry. ok is false when there is no target to
// reconcile toward (no pin and no discoverable image, or already latest).
func resolveDimensionTarget(
	cmd *cobra.Command,
	resolver versionresolver.Resolver,
	dimension versionDimension,
) (string, string, bool) {
	if dimension.pinnedVersion != "" {
		return normalizeVersionTag(dimension.pinnedVersion), "pinned via configuration", true
	}

	if dimension.imageRef == "" {
		return "", "", false
	}

	path, err := versionresolver.ComputeUpgradePath(
		cmd.Context(), resolver, dimension.imageRef, dimension.currentVersion, dimension.suffix,
	)
	if err != nil {
		if !errors.Is(err, versionresolver.ErrNoUpgradesAvailable) {
			notify.Warningf(cmd.ErrOrStderr(),
				"Cannot compute %s version drift: %v", dimension.label, err)
		}

		return "", "", false
	}

	return path[len(path)-1].Version.Original, "latest stable available upstream", true
}

// appendVersionChange records a version drift as a Change in the appropriate
// impact bucket (reboot-required for rolling OS upgrades, recreate-required
// otherwise) so it renders alongside the spec diff and counts toward --exit-code.
func appendVersionChange(
	mainDiff *clusterupdate.UpdateResult,
	dimension versionDimension,
	target, reason string,
) {
	change := clusterupdate.Change{
		Field:    strings.ToLower(dimension.label) + ".version",
		OldValue: dimension.currentVersion,
		NewValue: target,
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "version reconciliation (" + reason + ")",
	}

	if dimension.rolling {
		change.Category = clusterupdate.ChangeCategoryRebootRequired
		mainDiff.RebootRequired = append(mainDiff.RebootRequired, change)

		return
	}

	mainDiff.RecreateRequired = append(mainDiff.RecreateRequired, change)
}

// normalizeVersionTag ensures the tag carries a leading "v" so comparisons match
// the running-version format, mirroring normalizePinnedVersion's prefixing.
func normalizeVersionTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag != "" && !strings.HasPrefix(tag, "v") {
		return "v" + tag
	}

	return tag
}

// versionsEqual compares two version tags by parsed semver, falling back to a raw
// string match so a v-prefix mismatch never hides an actual no-op.
func versionsEqual(current, target string) bool {
	if current == target {
		return true
	}

	cur, curErr := versionresolver.ParseVersion(current)
	tgt, tgtErr := versionresolver.ParseVersion(target)

	return curErr == nil && tgtErr == nil && cur.Equal(tgt)
}

// isDowngrade reports whether target is strictly older than current. A pinned
// downgrade is not drift the reconciler would apply (update guards against it).
func isDowngrade(current, target string) bool {
	cur, curErr := versionresolver.ParseVersion(current)
	tgt, tgtErr := versionresolver.ParseVersion(target)

	return curErr == nil && tgtErr == nil && tgt.Less(cur)
}
