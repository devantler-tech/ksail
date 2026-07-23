package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

var errEKSComponentReconcilerUnavailable = errors.New("component reconciler is unavailable")

// persistRequiredEKSComponentState saves the declarative baseline needed to
// reconcile non-introspectable EKS component ownership. Unlike the wider
// best-effort spec snapshot, failure is fatal because a stale component
// baseline can make a later uninstall invisible.
func persistRequiredEKSComponentState(
	ctx *localregistry.Context,
	clusterName string,
	controllerManaged bool,
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
		AWSLoadBalancerControllerManaged:        controllerManaged,
		AWSLoadBalancerControllerServiceAccount: ctx.ClusterCfg.Spec.Cluster.EKS.AWSLoadBalancerControllerServiceAccount,
	}

	err := state.SaveEKSComponentState(clusterName, region, &snapshot)
	if err != nil {
		return fmt.Errorf("persist required EKS component state: %w", err)
	}

	return nil
}

// persistReconciledEKSComponentState records the ownership resulting from this
// update pass. When the controller was not touched, the reconciler returns the
// existing exact-region marker instead of inferring ownership from desired config.
func persistReconciledEKSComponentState(
	ctx *localregistry.Context,
	clusterName string,
	reconciler *componentReconciler,
) error {
	if ctx == nil || ctx.ClusterCfg == nil ||
		ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	if reconciler == nil {
		return fmt.Errorf(
			"persist required EKS component state: %w",
			errEKSComponentReconcilerUnavailable,
		)
	}

	controllerManaged, err := reconciler.eksLoadBalancerControllerManagedAfterReconcile()
	if err != nil {
		return err
	}

	return persistRequiredEKSComponentState(ctx, clusterName, controllerManaged)
}

// persistCreatedEKSComponentState verifies post-create GitOps ownership before
// recording a KSail marker. The shared Helm installer may succeed by skipping
// an externally managed release, which must remain unowned.
func persistCreatedEKSComponentState(
	goCtx context.Context,
	ctx *localregistry.Context,
	clusterName string,
) error {
	if ctx == nil || ctx.ClusterCfg == nil ||
		ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	controllerManaged, err := setup.EKSLoadBalancerControllerManagedByKSail(
		goCtx,
		ctx.ClusterCfg,
		getInstallerFactories(),
	)
	if err != nil {
		return fmt.Errorf("resolve EKS controller ownership after creation: %w", err)
	}

	return persistRequiredEKSComponentState(ctx, clusterName, controllerManaged)
}
