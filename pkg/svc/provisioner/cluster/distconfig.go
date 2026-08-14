package clusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// SimpleDistributionConfig returns a DistributionConfig for the distributions whose configuration is
// fully determined by the cluster name (K3s, VCluster, KWOK). It returns nil for distributions that
// need caller-specific construction (Vanilla, Talos, EKS, GKE, AKS), letting callers handle those
// themselves. This is shared by the operator and the local `ksail open web` backend so the name-only
// mappings live in one place.
func SimpleDistributionConfig(
	distribution v1alpha1.Distribution,
	name string,
) *DistributionConfig {
	//nolint:exhaustive // Vanilla, Talos, EKS, GKE, and AKS need caller-specific construction (return nil).
	switch distribution {
	case v1alpha1.DistributionK3s:
		return &DistributionConfig{K3d: k3dconfigmanager.NewK3dSimpleConfig(name, "", "")}
	case v1alpha1.DistributionVCluster:
		return &DistributionConfig{VCluster: &VClusterConfig{Name: name}}
	case v1alpha1.DistributionKWOK:
		return &DistributionConfig{KWOK: &KWOKConfig{Name: name}}
	default:
		return nil
	}
}

// BuildDistributionConfig builds the in-memory distribution config the factory needs for every
// distribution EXCEPT EKS, harmonizing the operator's `buildDistributionConfig` and the local
// `ksail open web` backend's distribution-config builder onto one implementation with the operator's
// stricter semantics:
//
//   - Vanilla (Kind): a Kind cluster named after the provisioned cluster, with the control-plane
//     node defaulted in when applyKindDefaults is set (the local backend needs it — an empty
//     v1alpha4.Cluster is rejected by Kind with "unknown apiVersion"; the operator does not default).
//   - Talos: a default config bundle named after the cluster, honoring the cluster's version pins via
//     ResolveKubernetesVersion (spec.cluster.talos.version / spec.cluster.kubernetesVersion) so a
//     create never deploys an incompatible Kubernetes version.
//   - K3s, VCluster, KWOK: the name-only configs from SimpleDistributionConfig.
//
// EKS is intentionally NOT handled here: the local backend renders an on-disk eksctl.yaml under
// ~/.ksail/clusters (with path-containment hardening) while the operator builds an in-memory config
// without a path, and the region is resolved from a backend-specific env var. BuildDistributionConfig
// returns (nil, nil) for EKS, GKE, and AKS so each caller builds its own EKSConfig/GKEConfig/
// AKSConfig; any other unsupported distribution returns clustererr.ErrUnsupportedDistribution.
//
// name is the already-resolved provisioned cluster name (the operator's controller.ProvisionedName or
// the local cluster name) so this package does not depend on either caller.
func BuildDistributionConfig(
	cluster *v1alpha1.Cluster,
	name string,
	applyKindDefaults bool,
) (*DistributionConfig, error) {
	distribution := cluster.Spec.Cluster.Distribution
	if distribution == "" {
		distribution = v1alpha1.DistributionVanilla
	}

	switch distribution {
	case v1alpha1.DistributionVanilla:
		return &DistributionConfig{Kind: newKindConfig(name, applyKindDefaults)}, nil
	case v1alpha1.DistributionTalos:
		return newTalosDistributionConfig(cluster, name)
	case v1alpha1.DistributionEKS:
		// EKS is backend-specific (on-disk config + env-var-resolved region); the caller builds it.
		return nil, nil //nolint:nilnil // (nil, nil) signals "EKS is caller-specific" (see doc).
	case v1alpha1.DistributionGKE:
		// GKE is backend-specific (env-var-resolved project/location + optional gke.yaml spec);
		// the caller builds it — same contract as EKS.
		return nil, nil //nolint:nilnil // (nil, nil) signals "GKE is caller-specific" (see doc).
	case v1alpha1.DistributionAKS:
		// AKS is backend-specific (env-var-resolved subscription/resource group + optional
		// aks.yaml spec); the caller builds it — same contract as EKS and GKE.
		return nil, nil //nolint:nilnil // (nil, nil) signals "AKS is caller-specific" (see doc).
	case v1alpha1.DistributionK3s, v1alpha1.DistributionVCluster, v1alpha1.DistributionKWOK:
		config := SimpleDistributionConfig(distribution, name)
		if config != nil {
			return config, nil
		}

		return nil, fmt.Errorf("%w: %q", clustererr.ErrUnsupportedDistribution, distribution)
	default:
		return nil, fmt.Errorf("%w: %q", clustererr.ErrUnsupportedDistribution, distribution)
	}
}

// newKindConfig builds a Kind cluster config named after the provisioned cluster. When applyDefaults
// is set the default control-plane node is added (NewKindCluster sets the TypeMeta;
// v1alpha4.SetDefaultsCluster adds the node) — required by the local backend, which would otherwise
// hand Kind a node-less config it rejects.
func newKindConfig(name string, applyDefaults bool) *v1alpha4.Cluster {
	kindCluster := kindconfigmanager.NewKindCluster(name, "", "")
	if applyDefaults {
		v1alpha4.SetDefaultsCluster(kindCluster)
	}

	return kindCluster
}

// newTalosDistributionConfig builds a Talos config bundle named after the provisioned cluster, honoring
// the cluster's Kubernetes version pin (or the Talos-compatible default). The cluster name is baked
// into the PKI, so it is regenerated via WithName.
func newTalosDistributionConfig(
	cluster *v1alpha1.Cluster,
	name string,
) (*DistributionConfig, error) {
	kubernetesVersion := talosconfigmanager.ResolveKubernetesVersion(
		cluster.Spec.Cluster.Talos.Version,
		cluster.Spec.Cluster.KubernetesVersion,
	)

	versionContract, err := talosconfigmanager.ParseVersionContract(
		cluster.Spec.Cluster.Talos.Version,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve Talos version contract: %w", err)
	}

	named, err := talosconfigmanager.NewDefaultConfigsWithVersionContractAndName(
		kubernetesVersion,
		name,
		versionContract,
	)
	if err != nil {
		return nil, fmt.Errorf("build talos distribution config: %w", err)
	}

	return &DistributionConfig{Talos: named}, nil
}
