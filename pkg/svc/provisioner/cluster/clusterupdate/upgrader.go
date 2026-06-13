package clusterupdate

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
)

// UpgraderMetadata is the static-per-call descriptor an Upgrader exposes for the
// version-reconciliation orchestrator: the OCI image references used for version
// discovery, the version pins implied by the distribution itself, and the image
// tag suffix.
//
// It is supplied by the distribution (not hard-coded in this package) so that
// distributions deriving pins dynamically — e.g. VCluster, which reads its pinned
// versions from the embedded SDK chart — populate the values at the point of
// construction or override the relevant accessor. Kind and K3d have fully static
// metadata; VCluster overrides the two pin accessors.
type UpgraderMetadata struct {
	// KubernetesImageRef is the OCI repository used for Kubernetes version
	// discovery (e.g. "kindest/node", "rancher/k3s"). Empty when the distribution
	// does not OCI-discover a Kubernetes version.
	KubernetesImageRef string
	// DistributionImageRef is the OCI repository used for distribution version
	// discovery (e.g. "ghcr.io/siderolabs/talos"). Empty when the distribution
	// version equals the Kubernetes version.
	DistributionImageRef string
	// PinnedDistributionVersion is the distribution version pinned for the cluster,
	// or "" when the distribution version is OCI-discovered.
	PinnedDistributionVersion string
	// PinnedKubernetesVersion is the Kubernetes version pinned implicitly by the
	// distribution itself, or "" when the Kubernetes version is OCI-discovered.
	PinnedKubernetesVersion string
	// VersionSuffix is the image-tag suffix the distribution uses (e.g. "k3s"),
	// or "" for plain semver tags.
	VersionSuffix string
}

// RecreationRequiredUpgrader is the shared embed for distributions that reach a
// target version by recreation rather than an in-place upgrade (Kind, K3d,
// VCluster). It supplies the two behavioral stubs that are byte-identical across
// those distributions — UpgradeKubernetes and UpgradeDistribution both return
// clustererr.ErrRecreationRequired so the orchestrator recreates at the target
// version — plus the five metadata accessors backed by the supplied
// UpgraderMetadata.
//
// Embedding types still implement GetCurrentVersions and PrepareConfigForVersion
// themselves (those carry genuine per-distribution logic), so a
// RecreationRequiredUpgrader alone is not a complete Upgrader. Distributions with
// pins that are not known until call time (VCluster's embedded chart version)
// override the relevant accessor; the rest inherit the static value.
type RecreationRequiredUpgrader struct {
	// distribution is the lower-case distribution name used in the
	// recreation-required error messages (e.g. "kind", "k3d", "vcluster").
	distribution string
	// metadata holds the static-per-call accessor values for the distribution.
	metadata UpgraderMetadata
}

// NewRecreationRequiredUpgrader builds a RecreationRequiredUpgrader for the named
// distribution with the supplied metadata. Distributions with dynamic pins pass
// the value resolved at construction (it is process-stable) and/or override the
// relevant accessor on the embedding type.
func NewRecreationRequiredUpgrader(
	distribution string,
	metadata UpgraderMetadata,
) RecreationRequiredUpgrader {
	return RecreationRequiredUpgrader{
		distribution: distribution,
		metadata:     metadata,
	}
}

// UpgradeKubernetes reports that an in-place Kubernetes upgrade is unsupported by
// returning clustererr.ErrRecreationRequired; the orchestrator recreates the
// cluster at the target version.
func (u RecreationRequiredUpgrader) UpgradeKubernetes(
	_ context.Context, _ string, _, _ string,
) error {
	return fmt.Errorf(
		"%s: in-place Kubernetes upgrade not supported: %w",
		u.distribution, clustererr.ErrRecreationRequired,
	)
}

// UpgradeDistribution reports that an in-place distribution upgrade is
// unsupported by returning clustererr.ErrRecreationRequired; the orchestrator
// recreates the cluster at the target version.
func (u RecreationRequiredUpgrader) UpgradeDistribution(
	_ context.Context, _ string, _, _ string,
) error {
	return fmt.Errorf(
		"%s: in-place distribution upgrade not supported: %w",
		u.distribution, clustererr.ErrRecreationRequired,
	)
}

// Metadata returns the distribution's upgrader metadata.
func (u RecreationRequiredUpgrader) Metadata() UpgraderMetadata {
	return u.metadata
}

// KubernetesImageRef returns the OCI repository used for Kubernetes version discovery.
func (u RecreationRequiredUpgrader) KubernetesImageRef() string {
	return u.metadata.KubernetesImageRef
}

// DistributionImageRef returns the OCI repository used for distribution version discovery.
func (u RecreationRequiredUpgrader) DistributionImageRef() string {
	return u.metadata.DistributionImageRef
}

// PinnedDistributionVersion returns the distribution version pinned for the cluster.
func (u RecreationRequiredUpgrader) PinnedDistributionVersion() string {
	return u.metadata.PinnedDistributionVersion
}

// PinnedKubernetesVersion returns the Kubernetes version pinned by the distribution itself.
func (u RecreationRequiredUpgrader) PinnedKubernetesVersion() string {
	return u.metadata.PinnedKubernetesVersion
}

// VersionSuffix returns the image-tag suffix the distribution uses.
func (u RecreationRequiredUpgrader) VersionSuffix() string {
	return u.metadata.VersionSuffix
}
