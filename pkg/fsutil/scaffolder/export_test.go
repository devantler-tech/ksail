package scaffolder

import "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"

// BuildVClusterAPIServerArgsForTest exposes buildVClusterAPIServerArgs for unit testing.
func BuildVClusterAPIServerArgsForTest(featureGates bool, oidc *v1alpha1.OIDCSpec) string {
	return buildVClusterAPIServerArgs(featureGates, oidc)
}
