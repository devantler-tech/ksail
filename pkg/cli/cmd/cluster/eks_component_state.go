package cluster

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

// persistRequiredEKSComponentState saves the declarative baseline needed to
// reconcile non-introspectable EKS component ownership. Unlike the wider
// best-effort spec snapshot, failure is fatal because a stale component
// baseline can make a later uninstall invisible.
func persistRequiredEKSComponentState(
	ctx *localregistry.Context,
	clusterName string,
) error {
	if ctx == nil || ctx.ClusterCfg == nil ||
		ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	if ctx.EKSConfig == nil {
		return fmt.Errorf(
			"persist required EKS component state: %w",
			errEKSConfigurationUnavailable,
		)
	}

	region := strings.TrimSpace(ctx.EKSConfig.Region)
	snapshot := state.EKSComponentState{
		Version:                                 state.EKSComponentStateVersion,
		ClusterName:                             clusterName,
		Region:                                  region,
		AWSLoadBalancerControllerManaged:        setup.NeedsLoadBalancerInstall(ctx.ClusterCfg),
		AWSLoadBalancerControllerServiceAccount: ctx.ClusterCfg.Spec.Cluster.EKS.AWSLoadBalancerControllerServiceAccount,
	}

	err := state.SaveEKSComponentState(clusterName, region, &snapshot)
	if err != nil {
		return fmt.Errorf("persist required EKS component state: %w", err)
	}

	return nil
}
