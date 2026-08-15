package scaffolder

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
)

// BuildVClusterAPIServerArgsForTest exposes buildVClusterAPIServerArgs for unit testing.
func BuildVClusterAPIServerArgsForTest(featureGates bool, oidc *v1alpha1.OIDCSpec) string {
	return buildVClusterAPIServerArgs(featureGates, oidc)
}

// BuildTalosGeneratorConfigForTest exposes buildTalosGeneratorConfig for unit testing.
func BuildTalosGeneratorConfigForTest(s *Scaffolder) *talosgenerator.Config {
	config, _ := s.buildTalosGeneratorConfig()

	return config
}
