package cluster

import "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"

// ExportShouldPushOCIArtifact exports shouldPushOCIArtifact for testing.
func ExportShouldPushOCIArtifact(clusterCfg *v1alpha1.Cluster) bool {
	return shouldPushOCIArtifact(clusterCfg)
}
