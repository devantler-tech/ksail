package lifecycle

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// ResolveAWSRegionWithSourceForTest exposes resolveAWSRegion, which also reports whether the
// region came from eks.yaml.
func ResolveAWSRegionWithSourceForTest(
	awsOpts v1alpha1.OptionsAWS,
	distCfg *clusterprovisioner.DistributionConfig,
) (string, bool) {
	return resolveAWSRegion(awsOpts, distCfg)
}

// ResolveClusterIdentityFromConfigForTest exposes resolveClusterIdentityFromConfig.
func ResolveClusterIdentityFromConfigForTest(
	resolved *ResolvedClusterInfo,
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
) {
	resolveClusterIdentityFromConfig(resolved, cfg, distCfg)
}

// HasLoadedEKSConfigSourceForTest exposes hasLoadedEKSConfigSource.
func HasLoadedEKSConfigSourceForTest(
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
	configClusterName string,
) bool {
	return hasLoadedEKSConfigSource(cfg, distCfg, configClusterName)
}

// ResolveClusterNameFromConfigForTest exposes resolveClusterNameFromConfig.
func ResolveClusterNameFromConfigForTest(
	currentName, configClusterName string,
	eksConfigSource bool,
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
) string {
	return resolveClusterNameFromConfig(
		currentName,
		configClusterName,
		eksConfigSource,
		cfg,
		distCfg,
	)
}

// ResolveConfigClusterNameForTest exposes resolveConfigClusterName.
func ResolveConfigClusterNameForTest(
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
) string {
	return resolveConfigClusterName(cfg, distCfg)
}
