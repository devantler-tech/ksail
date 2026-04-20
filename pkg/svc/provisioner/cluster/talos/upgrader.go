package talosprovisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/siderolabs/go-kubernetes/kubernetes/upgrade"
	"github.com/siderolabs/talos/pkg/cluster"
	k8s "github.com/siderolabs/talos/pkg/cluster/kubernetes"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/constants"
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

	clusterName = p.resolveClusterName(clusterName)
	installerImage := installerImageFromTag(toVersion)

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

	// Build the Talos client.
	talosClient, err := p.createTalosClient(ctx, cpNodeIP)
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

	talosVersion, err := p.getRunningTalosVersion(ctx, nodes[0].IP)
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

// KubernetesImageRef returns the Kubernetes apiserver image repository, which is
// used for version discovery. While Talos manages K8s versions internally through
// machine configuration, we need a registry to query for available versions.
func (p *Provisioner) KubernetesImageRef() string {
	return constants.KubernetesAPIServerImage
}

// DistributionImageRef returns the OCI repository for Talos node images.
func (p *Provisioner) DistributionImageRef() string {
	return talosImageRepository
}

// VersionSuffix returns an empty string since Talos uses plain semver tags.
func (p *Provisioner) VersionSuffix() string {
	return ""
}

// PrepareConfigForVersion is a no-op for Talos because it performs rolling
// upgrades via the SDK rather than cluster recreation.
func (p *Provisioner) PrepareConfigForVersion(_ string, _ string) error {
	return nil
}
