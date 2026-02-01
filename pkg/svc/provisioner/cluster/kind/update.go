package kindprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
)

// Update attempts to apply configuration changes to a running Kind cluster.
//
// Kind does NOT support in-place node modifications:
//   - Cannot add/remove control-plane nodes
//   - Cannot add/remove worker nodes
//   - Cannot change networking configuration
//   - Cannot modify containerd registry config
//
// The only updates possible are at the Kubernetes level (Helm components),
// which are handled by the installer layer, not the provisioner.
//
// For any structural Kind changes, this method returns RecreateRequired.
func (k *KindClusterProvisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts types.UpdateOptions,
) (*types.UpdateResult, error) {
	// Compute diff to identify what changed
	diff, err := k.DiffConfig(ctx, name, oldSpec, newSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to compute config diff: %w", err)
	}

	// For Kind, we can only report what would change - any structural
	// changes require cluster recreation
	if opts.DryRun || diff.HasRecreateRequired() {
		return diff, nil
	}

	// If there are only in-place changes (Helm components), those are handled
	// by the installer layer, not here
	return diff, nil
}

// DiffConfig computes configuration differences for Kind clusters.
// Since Kind doesn't support node-level changes, most changes are classified
// as RecreateRequired.
func (k *KindClusterProvisioner) DiffConfig(
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

	// MirrorsDir change requires recreate (containerd config is baked in)
	if oldSpec.Vanilla.MirrorsDir != newSpec.Vanilla.MirrorsDir {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "vanilla.mirrorsDir",
			OldValue: oldSpec.Vanilla.MirrorsDir,
			NewValue: newSpec.Vanilla.MirrorsDir,
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "Kind containerd registry mirrors are configured at cluster creation",
		})
	}

	// Node count changes require recreate
	// Kind node configuration comes from kind.yaml, not ClusterSpec
	// Changes to the Kind config (nodes, networking, etc.) require cluster recreation

	return result, nil
}

// GetCurrentConfig retrieves the current cluster configuration.
// For Kind clusters, we return the configuration used to create the cluster.
func (k *KindClusterProvisioner) GetCurrentConfig() (*v1alpha1.ClusterSpec, error) {
	// Kind doesn't persist configuration after creation.
	// Return the spec from the config file that was used.
	// This is a limitation of Kind - it doesn't store original config.
	return &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionVanilla,
		Provider:     v1alpha1.ProviderDocker,
		Vanilla: v1alpha1.OptionsVanilla{
			MirrorsDir: "", // Cannot retrieve from running cluster
		},
	}, nil
}
