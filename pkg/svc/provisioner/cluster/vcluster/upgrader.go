package vclusterprovisioner

import (
	"context"

	vclusterconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
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

// newRecreationUpgrader returns the shared recreation-based Upgrader behavior for
// VCluster. The static image refs and (empty) version suffix are supplied here;
// the two pin accessors are overridden on the Provisioner because VCluster reads
// them from the embedded SDK chart at call time. The embed supplies
// UpgradeKubernetes/UpgradeDistribution (both recreate); GetCurrentVersions and
// PrepareConfigForVersion below carry the genuine per-distribution logic.
func newRecreationUpgrader() clusterupdate.RecreationRequiredUpgrader {
	return clusterupdate.NewRecreationRequiredUpgrader("vcluster", clusterupdate.UpgraderMetadata{
		KubernetesImageRef:   kubernetesImageRepository,
		DistributionImageRef: distributionImageRepository,
	})
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

// PinnedDistributionVersion returns the embedded VCluster chart version
// (ChartVersion). VCluster's deployable version is baked into the SDK/Dockerfile
// and cannot be changed by cluster recreation, so the update flow must cap the
// distribution target at this pin rather than OCI-discovering the latest tag —
// otherwise it phantom-upgrades to a newer upstream release it cannot deliver and
// triggers a destructive no-op recreation that breaks update idempotency. It is
// computed per call rather than baked into the embed metadata so the value always
// reflects the current SDK chart.
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
