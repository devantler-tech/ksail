package kindprovisioner

import (
	"context"

	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Compile-time interface compliance check.
var _ clusterupdate.Upgrader = (*Provisioner)(nil)

// newRecreationUpgrader returns the shared recreation-based Upgrader behavior for
// Kind. Kind has no distribution version separate from its Kubernetes version, so
// its pins and distribution image ref are empty and it uses plain semver tags.
// The embed supplies UpgradeKubernetes/UpgradeDistribution (both recreate) and the
// five metadata accessors; GetCurrentVersions and PrepareConfigForVersion below
// carry the genuine per-distribution logic.
func newRecreationUpgrader() clusterupdate.RecreationRequiredUpgrader {
	return clusterupdate.NewRecreationRequiredUpgrader("kind", clusterupdate.UpgraderMetadata{
		KubernetesImageRef: "kindest/node",
	})
}

// GetCurrentVersions returns the Kubernetes and distribution versions for the cluster.
// It extracts the version tag from the configured Kind node image.
func (k *Provisioner) GetCurrentVersions(
	_ context.Context, _ string,
) (*clusterupdate.VersionInfo, error) {
	image := nodeImage(k)
	tag := clusterupdate.ExtractTag(image)

	return &clusterupdate.VersionInfo{
		KubernetesVersion:   tag,
		DistributionVersion: tag,
	}, nil
}

// PrepareConfigForVersion updates the Kind configuration to use the specified
// version so that a subsequent cluster recreation uses the new image.
func (k *Provisioner) PrepareConfigForVersion(_ string, version string) error {
	if k.kindConfig == nil {
		return nil
	}

	for i := range k.kindConfig.Nodes {
		k.kindConfig.Nodes[i].Image = "kindest/node:" + version
	}

	return nil
}

// nodeImage returns the Kind node image from the config, falling back to the default.
func nodeImage(k *Provisioner) string {
	if k.kindConfig != nil && len(k.kindConfig.Nodes) > 0 && k.kindConfig.Nodes[0].Image != "" {
		return k.kindConfig.Nodes[0].Image
	}

	return kindconfigmanager.DefaultKindNodeImage
}
