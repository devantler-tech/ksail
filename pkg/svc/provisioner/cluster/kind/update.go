package kindprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
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
func (k *Provisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	_ clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	diff, err := k.DiffConfig(ctx, name, oldSpec, newSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to compute config diff: %w", err)
	}

	// Kind cannot apply any structural changes in-place.
	// Component changes (Helm) are handled by the installer layer.
	return diff, nil
}

// DiffConfig computes configuration differences for Kind clusters.
// Since Kind doesn't support node-level changes, most changes are classified
// as RecreateRequired.
func (k *Provisioner) DiffConfig(
	_ context.Context,
	_ string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	// Kind clusters require recreation for most structural changes.
	result, ok := clusterupdate.NewDiffResult(oldSpec, newSpec)
	if !ok {
		return result, nil
	}

	// MirrorsDir change requires recreate (containerd config is baked in)
	if oldSpec.Vanilla.MirrorsDir != newSpec.Vanilla.MirrorsDir {
		result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
			Field:    "vanilla.mirrorsDir",
			OldValue: oldSpec.Vanilla.MirrorsDir,
			NewValue: newSpec.Vanilla.MirrorsDir,
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "Kind containerd registry mirrors are configured at cluster creation",
		})
	}

	// Node count changes require recreate
	// Kind node configuration comes from kind.yaml, not ClusterSpec
	// Changes to the Kind config (nodes, networking, etc.) require cluster recreation

	return result, nil
}

// GetCurrentConfig retrieves the current cluster configuration by probing the
// running cluster via Helm releases, Kubernetes Deployments, and Docker
// containers. Falls back to static defaults when no detector is available.
func (k *Provisioner) GetCurrentConfig(
	ctx context.Context,
) (*v1alpha1.ClusterSpec, error) {
	if k.componentDetector != nil {
		spec, err := k.componentDetector.DetectComponents(
			ctx,
			v1alpha1.DistributionVanilla,
			v1alpha1.ProviderDocker,
		)
		if err != nil {
			return nil, fmt.Errorf("detect components: %w", err)
		}

		clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

		return spec, nil
	}

	return clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	), nil
}
