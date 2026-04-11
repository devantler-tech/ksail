package talosprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clusterupdate"
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
		return fmt.Errorf("omni-managed clusters handle Talos upgrades externally")
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

// UpgradeKubernetes is not yet supported for Talos clusters.
// Talos manages Kubernetes versions through machine configuration and requires
// the full k8s.Upgrade SDK workflow (control plane component updates, kubelet
// upgrades, manifest syncing). This will be implemented in a future release.
func (p *Provisioner) UpgradeKubernetes(
	_ context.Context,
	_ string,
	_, _ string,
) error {
	return fmt.Errorf(
		"%w: Kubernetes version upgrades on Talos require the talosctl upgrade-k8s workflow; "+
			"use --update-distribution to upgrade Talos OS instead",
		ErrNotImplemented,
	)
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
		return nil, fmt.Errorf("no nodes found for cluster %s", clusterName)
	}

	talosVersion, err := p.getRunningTalosVersion(ctx, nodes[0].IP)
	if err != nil {
		return nil, fmt.Errorf("getting Talos version: %w", err)
	}

	k8sVersion := p.talosConfigs.KubernetesVersion()
	if k8sVersion != "" && k8sVersion[0] != 'v' {
		k8sVersion = "v" + k8sVersion
	}

	return &clusterupdate.VersionInfo{
		KubernetesVersion:   k8sVersion,
		DistributionVersion: talosVersion,
	}, nil
}

// KubernetesImageRef returns an empty string because Talos manages Kubernetes
// versions through its own machine configuration, not via a container image tag.
func (p *Provisioner) KubernetesImageRef() string {
	return ""
}

// DistributionImageRef returns the OCI repository for Talos node images.
func (p *Provisioner) DistributionImageRef() string {
	return talosImageRepository
}

// VersionSuffix returns an empty string since Talos uses plain semver tags.
func (p *Provisioner) VersionSuffix() string {
	return ""
}
