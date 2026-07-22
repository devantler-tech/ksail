package talosprovisioner

import (
	"context"
	"fmt"
	"strings"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/siderolabs/go-kubernetes/kubernetes/upgrade"
	"github.com/siderolabs/talos/pkg/cluster"
	k8s "github.com/siderolabs/talos/pkg/cluster/kubernetes"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	talosmachineryversion "github.com/siderolabs/talos/pkg/machinery/version"
)

// Compile-time interface compliance check.
var _ clusterupdate.Upgrader = (*Provisioner)(nil)

const (
	// talosImageRepository is the OCI repository for the Talos node image.
	talosImageRepository = "ghcr.io/siderolabs/talos"
)

// UpgradeDistribution performs a rolling Talos OS upgrade from fromVersion to
// toVersion using the LifecycleService API.
// Omni-managed clusters are skipped since Omni handles upgrades externally.
// Docker-provider (container-mode) clusters route to cluster recreation instead,
// because Talos cannot upgrade its OS in place inside a container — see below.
func (p *Provisioner) UpgradeDistribution(
	ctx context.Context,
	clusterName string,
	fromVersion, toVersion string,
) error {
	if p.omniOpts != nil {
		return fmt.Errorf(
			"talos upgrades are managed externally by Omni: %w",
			clustererr.ErrUpgradeSkipped,
		)
	}

	// Container-mode (Docker provider) Talos nodes cannot perform an in-place OS
	// upgrade. Talos masks out the Upgrade capability for container mode in its
	// capability matrix, so BOTH the legacy MachineService.Upgrade and the newer
	// LifecycleService.Upgrade (Talos >= 1.13) reject with
	// "FailedPrecondition: method is not supported in container mode". The OS
	// version of a Docker cluster is fixed by its node image at create time, so
	// the version is changed by recreating the cluster (like Kind/K3d/VCluster) —
	// signalled with ErrRecreationRequired. Recreation is gated on KSail being
	// able to provision the target: KSail generates machine configs with the
	// vendored pkg/machinery, so it cannot provision a Talos release newer than
	// that machinery; in that case skip with an actionable "update KSail" message
	// rather than recreating into a config KSail cannot generate.
	// (Routing mirrors Create/Delete/Exists: hetznerOpts==nil && omniOpts==nil =>
	// Docker; omniOpts is already excluded above.)
	if p.hetznerOpts == nil {
		if distributionVersionExceedsMachinerySupport(toVersion) {
			return fmt.Errorf(
				"this KSail build cannot provision Talos %s yet (it vendors Talos machinery %s); "+
					"update KSail or pin spec.cluster.talos.version to a supported version: %w",
				toVersion, talosmachineryversion.Tag, clustererr.ErrUpgradeSkipped,
			)
		}

		return fmt.Errorf(
			"in-place Talos OS upgrade is not supported for the Docker provider; "+
				"recreating the cluster to reach %s (from %s): %w",
			toVersion, fromVersion, clustererr.ErrRecreationRequired,
		)
	}

	clusterName = p.resolveClusterName(clusterName)
	// Use the schematic-aware installer image so the rolling OS upgrade preserves
	// configured system extensions (Image Factory installer), matching the
	// create/snapshot/autoscaler paths. Falls back to the bare upstream installer
	// only when no schematic is configured. See issue #5077.
	installerImage := p.resolveInstallerImage(toVersion)

	_, _ = fmt.Fprintf(p.logWriter,
		"  Upgrading Talos from %s to %s...\n", fromVersion, toVersion,
	)

	err := p.rollingUpgradeNodes(ctx, clusterName, installerImage, toVersion)
	if err != nil {
		return fmt.Errorf("rolling upgrade from %s to %s: %w", fromVersion, toVersion, err)
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  ✓ Talos upgraded to %s\n", toVersion,
	)

	return nil
}

// UpgradeKubernetes upgrades the Kubernetes control plane and kubelets on a Talos
// cluster using the Talos SDK's kubernetes.Upgrade() function. This handles:
// - Static pod upgrades (apiserver, controller-manager, scheduler)
// - Kube-proxy configuration patching
// - Rolling kubelet upgrades across all nodes
// - Kubernetes manifest sync (SSA for Talos >= 1.13)
//
// Omni-managed clusters are skipped since Omni handles K8s upgrades externally.
//
//nolint:funlen // sequential SDK workflow with setup, detection, and upgrade phases
func (p *Provisioner) UpgradeKubernetes(
	ctx context.Context,
	clusterName string,
	_, toVersion string,
) error {
	if p.omniOpts != nil {
		return fmt.Errorf(
			"kubernetes upgrades are managed externally by Omni: %w",
			clustererr.ErrUpgradeSkipped,
		)
	}

	clusterName = p.resolveClusterName(clusterName)

	// Get the first control-plane node to connect through.
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("listing nodes for K8s upgrade: %w", err)
	}

	var cpNodeIP string

	for _, n := range nodes {
		if n.Role == RoleControlPlane {
			cpNodeIP = n.IP

			break
		}
	}

	if cpNodeIP == "" {
		return fmt.Errorf("%w: %s", clustererr.ErrNoControlPlaneNodes, clusterName)
	}

	// Build the Talos client. The client is held across the multi-step K8s upgrade
	// workflow (static-pod upgrades, kubelet rollout), which cannot be safely
	// re-run wholesale, so the transient apid handshake race is absorbed by the
	// Version probe inside dialTalosClientWithRetry rather than retrying the flow.
	talosClient, err := p.dialTalosClientWithRetry(ctx, cpNodeIP, "kubernetes upgrade connect")
	if err != nil {
		return fmt.Errorf("creating Talos client for K8s upgrade: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	// Build the UpgradeProvider using SDK ready-made types.
	clientProvider := &cluster.ConfigClientProvider{
		DefaultClient: talosClient,
	}
	defer clientProvider.Close() //nolint:errcheck

	state := struct {
		cluster.ClientProvider
		cluster.K8sProvider
	}{
		ClientProvider: clientProvider,
		K8sProvider: &cluster.KubernetesClient{
			ClientProvider: clientProvider,
		},
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  Upgrading Kubernetes to %s...\n", toVersion,
	)

	// Strip the "v" prefix — the Talos SDK uses bare version numbers (e.g., "1.35.1").
	toVersionBare := strings.TrimPrefix(toVersion, "v")

	// Auto-detect the current running K8s version from the cluster.
	upgradeOpts := k8s.UpgradeOptions{
		LogOutput:              p.logWriter,
		PrePullImages:          true,
		UpgradeKubelet:         true,
		KubeletImage:           constants.KubeletImage,
		APIServerImage:         constants.KubernetesAPIServerImage,
		ControllerManagerImage: constants.KubernetesControllerManagerImage,
		SchedulerImage:         constants.KubernetesSchedulerImage,
		ProxyImage:             constants.KubeProxyImage,
		EncoderOpt: encoder.WithComments(
			encoder.CommentsDocs | encoder.CommentsExamples,
		),
	}

	fromVersionBare, err := k8s.DetectLowestVersion(ctx, &state, upgradeOpts)
	if err != nil {
		return fmt.Errorf("detecting current K8s version: %w", err)
	}

	upgradeOpts.Path, err = upgrade.NewPath(fromVersionBare, toVersionBare)
	if err != nil {
		return fmt.Errorf("creating upgrade path %s → %s: %w", fromVersionBare, toVersionBare, err)
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  Upgrade path: %s → %s\n", fromVersionBare, toVersionBare,
	)

	err = k8s.Upgrade(ctx, &state, upgradeOpts)
	if err != nil {
		return fmt.Errorf("K8s upgrade to %s failed: %w", toVersion, err)
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  ✓ Kubernetes upgraded to %s\n", toVersion,
	)

	return nil
}

// GetCurrentVersions returns the running Talos and Kubernetes versions.
func (p *Provisioner) GetCurrentVersions(
	ctx context.Context,
	clusterName string,
) (*clusterupdate.VersionInfo, error) {
	clusterName = p.resolveClusterName(clusterName)

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("listing nodes for version check: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: %s", clustererr.ErrNoNodesFound, clusterName)
	}

	// Reconcile from the cluster's LEAST-upgraded node rather than whichever node
	// is first. An interrupted rolling upgrade leaves the cluster in a mixed-version
	// state (some nodes already at the target, others still behind); reading a single
	// node lets an already-upgraded one mask the laggards, so the reconciler reports
	// "already at the pin" and silently stops — stranding the remaining nodes on the
	// old version. Mirrors the Kubernetes path, which reconciles from the cluster-wide
	// k8s.DetectLowestVersion.
	talosVersion, err := p.getLowestRunningTalosVersion(ctx, nodes)
	if err != nil {
		return nil, fmt.Errorf("getting Talos version: %w", err)
	}

	var k8sVersion string

	if p.talosConfigs != nil {
		k8sVersion = p.talosConfigs.KubernetesVersion()
	}

	if k8sVersion == "" {
		return nil, fmt.Errorf(
			"kubernetes version from Talos machine configs: %w", clustererr.ErrVersionUndetermined,
		)
	}

	if k8sVersion[0] != 'v' {
		k8sVersion = "v" + k8sVersion
	}

	return &clusterupdate.VersionInfo{
		KubernetesVersion:   k8sVersion,
		DistributionVersion: talosVersion,
	}, nil
}

// getLowestRunningTalosVersion returns the lowest (least-upgraded) running Talos
// OS version across all nodes. A rolling Talos upgrade replaces nodes one at a
// time, so an interrupted upgrade (or one a caller starts against a partially
// upgraded cluster) leaves nodes on different versions. The reconciler must treat
// the cluster as being at its least-upgraded node so the nodes still behind the
// target are rolled forward; sampling a single node instead reports whichever node
// happens to be first (workers are listed and upgraded first), letting an already
// upgraded node mask the laggards. This mirrors the Kubernetes path, which
// reconciles from the cluster-wide k8s.DetectLowestVersion.
func (p *Provisioner) getLowestRunningTalosVersion(
	ctx context.Context,
	nodes []nodeWithRole,
) (string, error) {
	tags := make([]string, 0, len(nodes))

	for _, node := range nodes {
		tag, err := p.getRunningTalosVersion(ctx, node.IP)
		if err != nil {
			return "", err
		}

		tags = append(tags, tag)
	}

	return lowestTalosVersion(tags)
}

// lowestTalosVersion returns the lowest tag in tags, compared as parsed semver.
// It errors when tags is empty or any tag is not parseable semver, so an
// undeterminable version fails the reconcile loudly rather than silently
// under-reporting the cluster as already at the target version.
func lowestTalosVersion(tags []string) (string, error) {
	var (
		lowestTag string
		lowestVer versionresolver.Version
	)

	for _, tag := range tags {
		ver, err := versionresolver.ParseVersion(tag)
		if err != nil {
			return "", fmt.Errorf("parsing running Talos version %q: %w", tag, err)
		}

		if lowestTag == "" || ver.Less(lowestVer) {
			lowestTag, lowestVer = tag, ver
		}
	}

	if lowestTag == "" {
		return "", fmt.Errorf("no running Talos versions to compare: %w",
			clustererr.ErrVersionUndetermined)
	}

	return lowestTag, nil
}

// KubernetesImageRef returns the Talos kubelet image repository used for version
// discovery. The kubelet is the Talos-specific artifact required by every
// Kubernetes upgrade, so its published tags are the safe availability boundary.
func (p *Provisioner) KubernetesImageRef() string {
	return constants.KubeletImage
}

// DistributionImageRef returns the OCI repository for Talos node images.
func (p *Provisioner) DistributionImageRef() string {
	return talosImageRepository
}

// PinnedDistributionVersion returns the Talos OS version the cluster should
// reconcile toward.
//
// An explicit spec.cluster.talos.version pin applies to every provider. When no
// pin is set, the result depends on the provider:
//
//   - Docker (container mode): nodes cannot upgrade their OS in place, so there
//     is no "follow the latest OCI version" rolling path. Instead the cluster
//     reconciles toward the Talos version this KSail build ships
//     (DefaultTalosImage) and recreates to reach it — keeping create and update
//     consistent and never provisioning a version KSail does not ship/test. To
//     move beyond the shipped version, pin spec.cluster.talos.version (honored up
//     to the vendored machinery version) or update KSail.
//   - Hetzner/Omni: real machines that upgrade in place, so they return "" here
//     and follow the latest discovered version (see DistributionImageRef).
//
// (Routing mirrors Create/Delete/Exists: hetznerOpts==nil && omniOpts==nil =>
// Docker.)
func (p *Provisioner) PinnedDistributionVersion() string {
	if p.talosOpts != nil {
		if pin := strings.TrimSpace(p.talosOpts.Version); pin != "" {
			return pin
		}
	}

	if p.hetznerOpts == nil && p.omniOpts == nil {
		return clusterupdate.ExtractTag(talosconfigmanager.DefaultTalosImage)
	}

	return ""
}

// PinnedKubernetesVersion returns "" because Talos follows the OCI-discovered
// Kubernetes version (or spec.cluster.kubernetesVersion when set), not an
// SDK-embedded pin.
func (p *Provisioner) PinnedKubernetesVersion() string {
	return ""
}

// VersionSuffix returns an empty string since Talos uses plain semver tags.
func (p *Provisioner) VersionSuffix() string {
	return ""
}

// PrepareConfigForVersion is a no-op for Talos. Hetzner/Omni upgrade in place
// (rolling upgrade via the SDK), so there is nothing to stage. The Docker
// recreate path rebuilds the cluster from ctx.ClusterCfg, where the target
// version already lives (spec.cluster.talos.version when pinned, or the shipped
// DefaultTalosImage when unset), so create reaches the right version without a
// separate config mutation here.
func (p *Provisioner) PrepareConfigForVersion(_ string, _ string) error {
	return nil
}

// distributionVersionExceedsMachinerySupport reports whether the requested Talos
// version is newer than the Talos machinery this KSail build vendors. KSail
// generates machine configs with the vendored pkg/machinery, so it cannot
// provision a Talos release newer than that machinery. talosmachineryversion.Tag
// is embedded from the machinery module at compile time. Unparseable versions are
// treated as within support so the create/validation path surfaces a real error
// rather than silently skipping.
func distributionVersionExceedsMachinerySupport(toVersion string) bool {
	target, err := versionresolver.ParseVersion(toVersion)
	if err != nil {
		return false
	}

	machinery, err := versionresolver.ParseVersion(talosmachineryversion.Tag)
	if err != nil {
		return false
	}

	return machinery.Less(target)
}
