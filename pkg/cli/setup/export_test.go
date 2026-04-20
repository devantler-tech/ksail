package setup

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	argocdgitops "github.com/devantler-tech/ksail/v7/pkg/client/argocd"
)

// NeedsInClusterConnectivityCheck exports needsInClusterConnectivityCheck for testing.
//
//nolint:gochecknoglobals // Standard Go export_test.go pattern.
var NeedsInClusterConnectivityCheck = needsInClusterConnectivityCheck

// APIServerStabilitySuccesses exports apiServerStabilitySuccesses for testing.
//
//nolint:gochecknoglobals // Standard Go export_test.go pattern.
var APIServerStabilitySuccesses = apiServerStabilitySuccesses

// InClusterConnectivityDeadline exports inClusterConnectivityDeadline for testing.
//
//nolint:gochecknoglobals // Standard Go export_test.go pattern.
var InClusterConnectivityDeadline = inClusterConnectivityDeadline

// Exported constants for test assertions.
const (
	APIServerStabilitySuccessesDefault = apiServerStabilitySuccessesDefault
	APIServerStabilitySuccessesFast    = apiServerStabilitySuccessesFast
	InClusterConnectivityTimeout       = inClusterConnectivityTimeout
	InClusterConnectivityTimeoutSlow   = inClusterConnectivityTimeoutSlow
)

// ClusterWithCNI creates a minimal Cluster config with the given CNI for testing.
func ClusterWithCNI(cni v1alpha1.CNI) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				CNI: cni,
			},
		},
	}
}

// BuildArgoCDEnsureOptions exports buildArgoCDEnsureOptions for testing.
func BuildArgoCDEnsureOptions(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	registryHost string,
) argocdgitops.EnsureOptions {
	return buildArgoCDEnsureOptions(clusterCfg, clusterName, registryHost)
}
