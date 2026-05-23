package clusterprovisioner

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
)

// SimpleDistributionConfig returns a DistributionConfig for the distributions whose configuration is
// fully determined by the cluster name (K3s, VCluster, KWOK). It returns nil for distributions that
// need caller-specific construction (Vanilla, Talos, EKS), letting callers handle those themselves.
// This is shared by the operator and the local `ksail ui` backend so the name-only mappings live in
// one place.
func SimpleDistributionConfig(
	distribution v1alpha1.Distribution,
	name string,
) *DistributionConfig {
	//nolint:exhaustive // Vanilla, Talos, and EKS need caller-specific construction (return nil).
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
