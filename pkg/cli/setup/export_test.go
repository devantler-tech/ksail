package setup

import "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"

// NeedsInClusterConnectivityCheck exports needsInClusterConnectivityCheck for testing.
var NeedsInClusterConnectivityCheck = needsInClusterConnectivityCheck //nolint:gochecknoglobals // Standard Go export_test.go pattern.

// APIServerStabilitySuccesses exports apiServerStabilitySuccesses for testing.
var APIServerStabilitySuccesses = apiServerStabilitySuccesses //nolint:gochecknoglobals // Standard Go export_test.go pattern.

// Exported constants for test assertions.
const (
	APIServerStabilitySuccessesDefault = apiServerStabilitySuccessesDefault
	APIServerStabilitySuccessesFast    = apiServerStabilitySuccessesFast
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
