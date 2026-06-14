package vclusterprovisioner

import (
	"context"
	"fmt"

	vclusterconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Compile-time interface compliance check.
var _ clusterupdate.Upgrader = (*Provisioner)(nil)

const (
	// kubernetesImageRepository is the OCI repository for VCluster Kubernetes images.
	kubernetesImageRepository = "ghcr.io/loft-sh/kubernetes"

	// distributionImageRepository is the OCI repository for the VCluster distribution image.
	distributionImageRepository = "ghcr.io/loft-sh/vcluster-pro"
)

// UpgradeKubernetes returns ErrRecreationRequired because VCluster does not
// support in-place Kubernetes version upgrades.
func (p *Provisioner) UpgradeKubernetes(_ context.Context, _ string, _, _ string) error {
	return fmt.Errorf(
		"vcluster: in-place Kubernetes upgrade not supported: %w", clustererr.ErrRecreationRequired,
	)
}

// UpgradeDistribution returns ErrRecreationRequired because VCluster does not
// support in-place distribution version upgrades.
func (p *Provisioner) UpgradeDistribution(_ context.Context, _ string, _, _ string) error {
	return fmt.Errorf(
		"vcluster: in-place distribution upgrade not supported: %w",
		clustererr.ErrRecreationRequired,
	)
}

// GetCurrentVersions returns the configured Kubernetes and VCluster chart versions.
func (p *Provisioner) GetCurrentVersions(
	_ context.Context,
	_ string,
) (*clusterupdate.VersionInfo, error) {
	return &clusterupdate.VersionInfo{
		KubernetesVersion:   vclusterconfigmanager.DefaultKubernetesVersion,
		DistributionVersion: vclusterconfigmanager.ChartVersion(),
	}, nil
}

// KubernetesImageRef returns the OCI repository for VCluster Kubernetes images.
func (p *Provisioner) KubernetesImageRef() string {
	return kubernetesImageRepository
}

// DistributionImageRef returns the OCI repository for the VCluster distribution image.
func (p *Provisioner) DistributionImageRef() string {
	return distributionImageRepository
}

// PinnedDistributionVersion returns the embedded VCluster chart version
// (ChartVersion). VCluster's deployable version is baked into the SDK/Dockerfile
// and cannot be changed by cluster recreation, so the update flow must cap the
// distribution target at this pin rather than OCI-discovering the latest tag —
// otherwise it phantom-upgrades to a newer upstream release it cannot deliver and
// triggers a destructive no-op recreation that breaks update idempotency.
func (p *Provisioner) PinnedDistributionVersion() string {
	return vclusterconfigmanager.ChartVersion()
}

// PinnedKubernetesVersion returns the embedded VCluster Kubernetes version
// (DefaultKubernetesVersion). Like the chart version it is baked into the SDK
// image and unreachable by recreation, so it is capped here to keep
// `ksail cluster update` idempotent across upstream Kubernetes releases.
func (p *Provisioner) PinnedKubernetesVersion() string {
	return vclusterconfigmanager.DefaultKubernetesVersion
}

// VersionSuffix returns an empty string since VCluster uses plain semver tags.
func (p *Provisioner) VersionSuffix() string {
	return ""
}

// PrepareConfigForVersion is a no-op for VCluster because versions are
// determined by the embedded SDK (ChartVersion) and module constants
// (DefaultKubernetesVersion). Recreation-based upgrades require updating
// the vcluster Go dependency in go.mod to pull a newer SDK version.
func (p *Provisioner) PrepareConfigForVersion(upgradeType string, version string) error {
	// VCluster config is managed by the chart values; the recreation flow
	// re-reads chart defaults. No in-memory config update needed.
	_ = upgradeType
	_ = version

	return nil
}
