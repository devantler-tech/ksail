package kindprovisioner

import (
	"context"
	"fmt"

	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Compile-time interface compliance check.
var _ clusterupdate.Upgrader = (*Provisioner)(nil)

// UpgradeKubernetes returns ErrRecreationRequired because Kind does not support
// in-place Kubernetes version changes. The orchestrator handles recreation.
func (k *Provisioner) UpgradeKubernetes(_ context.Context, _ string, _, _ string) error {
	return fmt.Errorf(
		"kind: in-place Kubernetes upgrade not supported: %w",
		clustererr.ErrRecreationRequired,
	)
}

// UpgradeDistribution returns ErrRecreationRequired because Kind does not support
// in-place distribution version changes. The orchestrator handles recreation.
func (k *Provisioner) UpgradeDistribution(_ context.Context, _ string, _, _ string) error {
	return fmt.Errorf(
		"kind: in-place distribution upgrade not supported: %w",
		clustererr.ErrRecreationRequired,
	)
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

// KubernetesImageRef returns the OCI image repository for Kind node images.
func (k *Provisioner) KubernetesImageRef() string {
	return "kindest/node"
}

// DistributionImageRef returns an empty string because Kind's distribution
// version is the same as the Kubernetes version — there is no separate
// distribution image. The `--update-distribution` flag is intentionally a
// no-op for Kind; use `--update-kubernetes` to upgrade both.
func (k *Provisioner) DistributionImageRef() string {
	return ""
}

// VersionSuffix returns an empty string because Kind uses plain semver tags.
func (k *Provisioner) VersionSuffix() string {
	return ""
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
