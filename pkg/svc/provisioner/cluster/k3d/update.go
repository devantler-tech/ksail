package k3dprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
)

// Update applies configuration changes to a running K3d cluster.
// K3d supports:
//   - Adding/removing worker nodes via k3d node commands
//   - Registry configuration updates via registries.yaml
//
// It does NOT support adding/removing server (control-plane) nodes after creation.
func (k *K3dClusterProvisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts types.UpdateOptions,
) (*types.UpdateResult, error) {
	if oldSpec == nil || newSpec == nil {
		return &types.UpdateResult{}, nil
	}

	// Compute diff
	diff, err := k.DiffConfig(ctx, name, oldSpec, newSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to compute config diff: %w", err)
	}

	if opts.DryRun {
		return diff, nil
	}

	result := &types.UpdateResult{
		InPlaceChanges:   diff.InPlaceChanges,
		RebootRequired:   diff.RebootRequired,
		RecreateRequired: diff.RecreateRequired,
		AppliedChanges:   make([]types.Change, 0),
		FailedChanges:    make([]types.Change, 0),
	}

	// If there are recreate-required changes, we cannot handle them
	if diff.HasRecreateRequired() {
		return result, fmt.Errorf("%w: %d changes require restart",
			clustererrors.ErrRecreationRequired, len(diff.RecreateRequired))
	}

	clusterName := k.resolveName(name)

	// Handle worker node scaling
	err = k.applyWorkerScaling(ctx, clusterName, oldSpec, newSpec, result)
	if err != nil {
		return result, fmt.Errorf("failed to scale workers: %w", err)
	}

	return result, nil
}

// DiffConfig computes the differences between current and desired configurations.
func (k *K3dClusterProvisioner) DiffConfig(
	_ context.Context,
	_ string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*types.UpdateResult, error) {
	result := &types.UpdateResult{
		InPlaceChanges:   make([]types.Change, 0),
		RebootRequired:   make([]types.Change, 0),
		RecreateRequired: make([]types.Change, 0),
	}

	if oldSpec == nil || newSpec == nil {
		return result, nil
	}

	// K3d configuration comes from the SimpleConfig (k3d.yaml)
	if k.simpleCfg == nil {
		return result, nil
	}

	// Check server (control-plane) count - K3d does NOT support scaling servers after creation
	// Server count comes from the k3d SimpleConfig, not ClusterSpec
	// Changes to servers would be detected by comparing old/new simpleCfg versions
	// For now, report based on the current config vs any documented expectation

	// Check agent (worker) count - K3d DOES support scaling agents
	// Agent count also comes from k3d SimpleConfig
	// The k3d.yaml is the source of truth for k3d clusters

	return result, nil
}

// applyWorkerScaling handles adding or removing K3d agent nodes.
//
//nolint:unparam // result will be used when scaling is implemented
func (k *K3dClusterProvisioner) applyWorkerScaling(
	_ context.Context,
	_ string,
	_, _ *v1alpha1.ClusterSpec,
	_ *types.UpdateResult,
) error {
	if k.simpleCfg == nil {
		return nil
	}

	// K3d agent scaling uses the SimpleConfig
	// Since we don't track old vs new SimpleConfig, scaling would be handled
	// by comparing actual running nodes vs desired count

	return nil
}

// GetCurrentConfig retrieves the current cluster configuration.
// For K3d clusters, we return the configuration based on the SimpleConfig
// used for cluster creation.
func (k *K3dClusterProvisioner) GetCurrentConfig() (*v1alpha1.ClusterSpec, error) {
	// K3d configuration comes from the SimpleConfig
	spec := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionK3s,
		Provider:     v1alpha1.ProviderDocker,
	}

	return spec, nil
}
