package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/siderolabs/go-kubernetes/kubernetes/ssa"
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

	// kubernetesUpgradeReconcileTimeout mirrors talosctl's default for library
	// callers. Without it the SDK receives zero instead, so Kubernetes 1.37 can
	// exhaust manifest reconciliation while its API server restarts and
	// kube-proxy rolls out.
	kubernetesUpgradeReconcileTimeout = 5 * time.Minute
)

var errAutoscalerNodeConfigurationChangesFailed = errors.New(
	"autoscaler node configuration changes failed",
)

// kubernetesUpgradeOptions supplies the defaults that talosctl normally adds
// around the lower-level SDK. KSail calls that SDK directly, so Go zero values
// here would silently change reconciliation and inventory behavior.
func kubernetesUpgradeOptions(logWriter io.Writer) k8s.UpgradeOptions {
	return k8s.UpgradeOptions{
		LogOutput:              logWriter,
		PrePullImages:          true,
		UpgradeKubelet:         true,
		KubeletImage:           constants.KubeletImage,
		APIServerImage:         constants.KubernetesAPIServerImage,
		ControllerManagerImage: constants.KubernetesControllerManagerImage,
		SchedulerImage:         constants.KubernetesSchedulerImage,
		ProxyImage:             constants.KubeProxyImage,
		ReconcileTimeout:       kubernetesUpgradeReconcileTimeout,
		InventoryPolicy:        ssa.InventoryPolicyAdoptIfNoInventory,
		EncoderOpt: encoder.WithComments(
			encoder.CommentsDocs | encoder.CommentsExamples,
		),
	}
}

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

	// The nodes pull that installer from Image Factory, which serves a computed schematic
	// only once it has been registered (#7132).
	err := p.ensureSchematicRegistered(ctx, p.resolveSchematicID())
	if err != nil {
		return err
	}

	// A same-version image roll runs before the regular config diff. Explicit
	// schematic IDs are absent from rendered Talos configs, so that diff may be
	// clean and skip the usual autoscaler Secret refresh. Establish the new
	// snapshot baseline and recycle old autoscaler nodes before static nodes roll.
	if runningVersionMatchesTarget(fromVersion, toVersion) {
		err = p.reconcileAutoscalerImageBaseline(ctx, clusterName)
		if err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  Upgrading Talos from %s to %s...\n", fromVersion, toVersion,
	)

	err = p.rollingUpgradeNodes(ctx, clusterName, installerImage, toVersion)
	if err != nil {
		return fmt.Errorf("rolling upgrade from %s to %s: %w", fromVersion, toVersion, err)
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  ✓ Talos upgraded to %s\n", toVersion,
	)

	return nil
}

func (p *Provisioner) reconcileAutoscalerImageBaseline(
	ctx context.Context,
	clusterName string,
) error {
	result := clusterupdate.NewEmptyUpdateResult()
	// Start clean: an earlier update on this provisioner may have failed before its
	// classified pass consumed the marker.
	p.autoscalerSecretRefreshedEarly = false

	// A fresh invocation has newly generated PKI. The normal Update path syncs
	// from a running control plane before writing the autoscaler Secret; the
	// same-version image path must do so as well, including its live endpoint.
	err := p.syncSecretsFromCluster(ctx, clusterName, nil, nil, result)
	if err != nil {
		return fmt.Errorf("syncing cluster identity for autoscaler image baseline: %w", err)
	}

	err = p.ensureAutoscalerSecretIfNeeded(ctx, clusterName, nil, result)
	if err != nil {
		return fmt.Errorf("reconciling autoscaler image baseline: %w", err)
	}

	// Inventory reports (a server of a removed pool or of a disabled autoscaler) do
	// not stop the static nodes from rolling; the regular update reports them.
	if failed := autoscalerConvergenceFailures(result); failed > 0 {
		return fmt.Errorf("reconciling autoscaler image baseline: %d changes failed: %w",
			failed, errAutoscalerNodeConfigurationChangesFailed)
	}

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

	// The client is held across the multi-step K8s upgrade workflow (static-pod
	// upgrades, kubelet rollout), which cannot be safely re-run wholesale, so the
	// transient apid handshake race is absorbed by the Version probe inside
	// dialTalosClientWithRetry rather than retrying the flow.
	err = p.withKubernetesUpgradeProvider(ctx, cpNodeIP, "kubernetes upgrade connect",
		func(state k8s.UpgradeProvider) error {
			_, _ = fmt.Fprintf(p.logWriter,
				"  Upgrading Kubernetes to %s...\n", toVersion,
			)

			// Strip the "v" prefix — the Talos SDK uses bare version numbers (e.g., "1.35.1").
			toVersionBare := strings.TrimPrefix(toVersion, "v")

			// Auto-detect the current running K8s version from the cluster.
			upgradeOpts := kubernetesUpgradeOptions(p.logWriter)

			fromVersionBare, upgradeErr := k8s.DetectLowestVersion(ctx, state, upgradeOpts)
			if upgradeErr != nil {
				return fmt.Errorf("detecting current K8s version: %w", upgradeErr)
			}

			upgradeOpts.Path, upgradeErr = upgrade.NewPath(fromVersionBare, toVersionBare)
			if upgradeErr != nil {
				return fmt.Errorf(
					"creating upgrade path %s → %s: %w", fromVersionBare, toVersionBare, upgradeErr,
				)
			}

			_, _ = fmt.Fprintf(p.logWriter,
				"  Upgrade path: %s → %s\n", fromVersionBare, toVersionBare,
			)

			upgradeErr = k8s.Upgrade(ctx, state, upgradeOpts)
			if upgradeErr != nil {
				return fmt.Errorf("K8s upgrade to %s failed: %w", toVersion, upgradeErr)
			}

			return nil
		})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  ✓ Kubernetes upgraded to %s\n", toVersion,
	)

	return nil
}

// withKubernetesUpgradeProvider connects to the control-plane node at cpNodeIP and
// runs action with the Talos SDK's Kubernetes upgrade provider for that connection.
// The planner and the upgrade step share it, so both read the running version
// through the same client.
func (p *Provisioner) withKubernetesUpgradeProvider(
	ctx context.Context,
	cpNodeIP, description string,
	action func(state k8s.UpgradeProvider) error,
) error {
	talosClient, err := p.dialTalosClientWithRetry(ctx, cpNodeIP, description)
	if err != nil {
		return fmt.Errorf("creating Talos client for %s: %w", description, err)
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
		K8sProvider: kubernetesUpgradeProvider(&cluster.KubernetesClient{
			ClientProvider: clientProvider,
		}),
	}
	defer state.K8sClose() //nolint:errcheck

	return action(&state)
}

// detectRunningKubernetesVersion returns the lowest Kubernetes version running on
// the cluster's control-plane components, read through the control-plane node at
// cpNodeIP. It is the same detection the upgrade step uses, so the planned path
// starts where the upgrade will.
func (p *Provisioner) detectRunningKubernetesVersion(
	ctx context.Context,
	cpNodeIP string,
) (string, error) {
	if p.kubernetesVersionDetector != nil {
		return p.kubernetesVersionDetector(ctx, cpNodeIP)
	}

	var version string

	err := p.withKubernetesUpgradeProvider(ctx, cpNodeIP, "kubernetes version check",
		func(state k8s.UpgradeProvider) error {
			detected, err := k8s.DetectLowestVersion(
				ctx,
				state,
				kubernetesUpgradeOptions(io.Discard),
			)
			if err != nil {
				return fmt.Errorf("detecting running K8s version: %w", err)
			}

			version = detected

			return nil
		})
	if err != nil {
		return "", err
	}

	return version, nil
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

	k8sVersion, err := p.getLowestRunningKubernetesVersion(ctx, nodes)
	if err != nil {
		return nil, err
	}

	return &clusterupdate.VersionInfo{
		KubernetesVersion:   k8sVersion,
		DistributionVersion: talosVersion,
	}, nil
}

// getLowestRunningKubernetesVersion returns the lowest Kubernetes version the
// cluster actually runs, with a "v" prefix. It never falls back to the rendered
// machine config: that keeps the version KSail first generated, so after an
// automatic upgrade it lags the cluster and the planner would start the next
// path from a version the cluster has already left (ksail#7412).
func (p *Provisioner) getLowestRunningKubernetesVersion(
	ctx context.Context,
	nodes []nodeWithRole,
) (string, error) {
	var cpNodeIP string

	for _, n := range nodes {
		if n.Role == RoleControlPlane {
			cpNodeIP = n.IP

			break
		}
	}

	if cpNodeIP == "" {
		return "", fmt.Errorf(
			"getting Kubernetes version: %w", clustererr.ErrNoControlPlaneNodes,
		)
	}

	k8sVersion, err := p.detectRunningKubernetesVersion(ctx, cpNodeIP)
	if err != nil {
		return "", fmt.Errorf("getting Kubernetes version: %w", err)
	}

	k8sVersion = strings.TrimSpace(k8sVersion)
	if k8sVersion == "" {
		return "", fmt.Errorf(
			"kubernetes version from the running cluster: %w", clustererr.ErrVersionUndetermined,
		)
	}

	if k8sVersion[0] != 'v' {
		k8sVersion = "v" + k8sVersion
	}

	return k8sVersion, nil
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
