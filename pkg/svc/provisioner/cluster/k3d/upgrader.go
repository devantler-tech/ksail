package k3dprovisioner

import (
	"context"

	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Compile-time interface compliance check.
var _ clusterupdate.Upgrader = (*Provisioner)(nil)

// newRecreationUpgrader returns the shared recreation-based Upgrader behavior for
// K3d. K3d's distribution version is the Kubernetes version (the rancher/k3s image
// bundles both), so it has no distribution image ref or pins; it uses the "k3s"
// tag suffix (e.g. "v1.35.3-k3s1"). The embed supplies
// UpgradeKubernetes/UpgradeDistribution (both recreate) and the five metadata
// accessors; GetCurrentVersions and PrepareConfigForVersion below carry the
// genuine per-distribution logic.
func newRecreationUpgrader() clusterupdate.RecreationRequiredUpgrader {
	return clusterupdate.NewRecreationRequiredUpgrader("k3d", clusterupdate.UpgraderMetadata{
		KubernetesImageRef: "rancher/k3s",
		VersionSuffix:      "k3s",
	})
}

// GetCurrentVersions returns the Kubernetes and distribution versions for the cluster.
// For K3s, both versions are the same. The version is extracted from the configured
// K3s image tag (e.g., "v1.35.3-k3s1" from "rancher/k3s:v1.35.3-k3s1").
func (k *Provisioner) GetCurrentVersions(
	_ context.Context, _ string,
) (*clusterupdate.VersionInfo, error) {
	image := k3sImage(k)
	tag := clusterupdate.ExtractTag(image)

	return &clusterupdate.VersionInfo{
		KubernetesVersion:   tag,
		DistributionVersion: tag,
	}, nil
}

// PrepareConfigForVersion updates the K3d configuration to use the specified
// version so that a subsequent cluster recreation uses the new image.
func (k *Provisioner) PrepareConfigForVersion(_ string, version string) error {
	if k.simpleCfg != nil {
		k.simpleCfg.Image = "rancher/k3s:" + version
	}

	return nil
}

// k3sImage returns the K3s image from the config, falling back to the default.
func k3sImage(k *Provisioner) string {
	if k.simpleCfg != nil && k.simpleCfg.Image != "" {
		return k.simpleCfg.Image
	}

	return k3dconfigmanager.DefaultK3sImage
}
