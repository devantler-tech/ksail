package k3dprovisioner

import (
	"context"
	"fmt"

	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Compile-time interface compliance check.
var _ clusterupdate.Upgrader = (*Provisioner)(nil)

// UpgradeKubernetes returns ErrRecreationRequired because K3d does not support
// in-place Kubernetes version changes. The orchestrator handles recreation.
func (p *Provisioner) UpgradeKubernetes(_ context.Context, _ string, _, _ string) error {
	return fmt.Errorf(
		"k3d: in-place Kubernetes upgrade not supported: %w", clustererr.ErrRecreationRequired,
	)
}

// UpgradeDistribution returns ErrRecreationRequired because K3d does not support
// in-place distribution version changes. The orchestrator handles recreation.
func (p *Provisioner) UpgradeDistribution(_ context.Context, _ string, _, _ string) error {
	return fmt.Errorf(
		"k3d: in-place distribution upgrade not supported: %w", clustererr.ErrRecreationRequired,
	)
}

// GetCurrentVersions returns the Kubernetes and distribution versions for the cluster.
// For K3s, both versions are the same. The version is extracted from the configured
// K3s image tag (e.g., "v1.35.3-k3s1" from "rancher/k3s:v1.35.3-k3s1").
func (p *Provisioner) GetCurrentVersions(
	_ context.Context, _ string,
) (*clusterupdate.VersionInfo, error) {
	image := k3sImage(p)
	tag := clusterupdate.ExtractTag(image)

	return &clusterupdate.VersionInfo{
		KubernetesVersion:   tag,
		DistributionVersion: tag,
	}, nil
}

// KubernetesImageRef returns the OCI image repository for K3s images.
func (p *Provisioner) KubernetesImageRef() string {
	return "rancher/k3s"
}

// DistributionImageRef returns an empty string because K3d's distribution
// version is the same as the Kubernetes version — the rancher/k3s image
// bundles both. The `--update-distribution` flag is intentionally a no-op
// for K3d; use `--update-kubernetes` to upgrade both.
func (p *Provisioner) DistributionImageRef() string {
	return ""
}

// VersionSuffix returns "k3s" to match K3s image tags like "v1.35.3-k3s1".
func (p *Provisioner) VersionSuffix() string {
	return "k3s"
}

// PrepareConfigForVersion updates the K3d configuration to use the specified
// version so that a subsequent cluster recreation uses the new image.
func (p *Provisioner) PrepareConfigForVersion(_ string, version string) error {
	if p.simpleCfg != nil {
		p.simpleCfg.Image = "rancher/k3s:" + version
	}

	return nil
}

// k3sImage returns the K3s image from the config, falling back to the default.
func k3sImage(p *Provisioner) string {
	if p.simpleCfg != nil && p.simpleCfg.Image != "" {
		return p.simpleCfg.Image
	}

	return k3dconfigmanager.DefaultK3sImage
}
