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
	releaseIdentity string,
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
		Version:                                  state.EKSComponentStateVersion,
		ClusterName:                              clusterName,
		Region:                                   region,
		AWSLoadBalancerControllerManaged:         controllerManaged,
		AWSLoadBalancerControllerReleaseIdentity: releaseIdentity,
		AWSLoadBalancerControllerServiceAccount:  ctx.ClusterCfg.Spec.Cluster.EKS.AWSLoadBalancerControllerServiceAccount,
	}

	err := state.SaveEKSComponentState(clusterName, region, &snapshot)
	if err != nil {
		return fmt.Errorf("persist required EKS component state: %w", err)
	}

	return nil
}

// clearDeletedEKSState invalidates every state artifact that could otherwise
// be mistaken for the replacement EKS cluster. It runs after deletion and
// before creation so a failed recreate cannot retain ownership of a cluster
// that no longer exists.
func clearDeletedEKSState(ctx *localregistry.Context, clusterName string) error {
	if ctx == nil || ctx.ClusterCfg == nil ||
		ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	if ctx.EKSConfig == nil {
		return fmt.Errorf(
			"clear deleted EKS state: %w",
			errEKSConfigurationUnavailable,
		)
	}

	region := strings.TrimSpace(ctx.EKSConfig.Region)
	if region == "" {
		return fmt.Errorf("clear deleted EKS state: %w", state.ErrInvalidRegion)
	}

	err := state.DeleteEKSRegionState(clusterName, region)
	if err != nil {
		return fmt.Errorf("clear deleted EKS state: %w", err)
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

	controllerManaged, releaseIdentity, err := reconciler.eksLoadBalancerControllerOwnershipAfterReconcile()
	if err != nil {
		return err
	}

	return persistRequiredEKSComponentState(
		ctx,
		clusterName,
		controllerManaged,
		releaseIdentity,
	)
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

	controllerManaged, releaseIdentity, err := setup.EKSLoadBalancerControllerManagedByKSail(
		goCtx,
		ctx.ClusterCfg,
		getInstallerFactories(),
	)
	if err != nil {
		return fmt.Errorf("resolve EKS controller ownership after creation: %w", err)
	}

	return persistRequiredEKSComponentState(
		ctx,
		clusterName,
		controllerManaged,
		releaseIdentity,
	)
}

// overlayOwnedEKSControllerCleanupBaseline turns positive persisted ownership
// into a removal-only diff signal when the desired controller is disabled.
// Live deployed status remains authoritative when the controller is desired,
// so an absent release still produces an install diff. On disable, however, an
// owned failed/pending Helm revision must reach the identity-checked uninstall
// path even though normal component detection reports it inactive.
func overlayOwnedEKSControllerCleanupBaseline(
	currentSpec, desiredSpec *v1alpha1.ClusterSpec,
	clusterName, region string,
) error {
	if currentSpec == nil || desiredSpec == nil ||
		desiredSpec.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	desired := &v1alpha1.Cluster{}

	desired.Spec.Cluster = *desiredSpec
	if setup.NeedsLoadBalancerInstall(desired) {
		return nil
	}

	snapshot, err := state.LoadEKSComponentState(clusterName, region)
	if err != nil {
		if errors.Is(err, state.ErrEKSComponentStateNotFound) {
			return nil
		}

		return fmt.Errorf("load EKS controller cleanup ownership: %w", err)
	}

	if !snapshot.AWSLoadBalancerControllerManaged {
		return nil
	}

	currentSpec.LoadBalancer = v1alpha1.LoadBalancerEnabled
	currentSpec.EKS.ExperimentalAWSLoadBalancerController = true

	return nil
}
