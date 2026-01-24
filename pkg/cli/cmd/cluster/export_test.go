package cluster

import (
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// ExportShouldPushOCIArtifact exports ShouldPushOCIArtifact for testing.
func ExportShouldPushOCIArtifact(clusterCfg *v1alpha1.Cluster) bool {
	return setup.ShouldPushOCIArtifact(clusterCfg)
}

// ExportSetupK3dCSI exports SetupK3dCSI for testing.
func ExportSetupK3dCSI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	SetupK3dCSI(clusterCfg, k3dConfig)
}
