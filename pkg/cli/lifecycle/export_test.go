package lifecycle

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// ResolveAWSRegion exports resolveAWSRegion for testing.
func ResolveAWSRegion(
	awsOpts v1alpha1.OptionsAWS,
	distCfg *clusterprovisioner.DistributionConfig,
) string {
	return resolveAWSRegion(awsOpts, distCfg)
}
