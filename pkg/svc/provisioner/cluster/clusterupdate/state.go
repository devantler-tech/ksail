package clusterupdate

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

// MergePersistedState loads the ClusterSpec previously saved by create/update
// (pkg/svc/state) and merges the fields that cannot be introspected from a live
// cluster onto the supplied baseline spec. This prevents the diff engine from
// reporting false recreate-required changes for boot-time or registry settings
// that are not exposed by any cluster API.
//
// The merged fields are:
//   - Talos.ISO — a Hetzner Cloud ISO ID baked in at boot, undetectable at runtime.
//   - LocalRegistry — local registry configuration not exposed by any cluster API.
//   - Vanilla.MirrorsDir — the containerd mirrors directory baked into the Kind
//     node's containerd config at creation (Kind/K3s baselines otherwise carry the
//     zero value, so a configured mirrorsDir reads as recreate-required every run;
//     see kind.DiffConfig and diff/engine.go's documented LocalRegistry limitation).
//   - EKS.ExperimentalAWSLoadBalancerController — an explicit component ownership
//     choice that cannot be inferred from generic component detection alone.
//
// It is a no-op (returns nil) when no state exists for clusterName
// (state.ErrStateNotFound). Any other failure (I/O, corrupt JSON, permission)
// is returned wrapped so the caller can surface it. spec must be non-nil.
func MergePersistedState(spec *v1alpha1.ClusterSpec, clusterName string) error {
	if spec == nil {
		return nil
	}

	saved, err := state.LoadClusterSpec(clusterName)
	if err != nil {
		if errors.Is(err, state.ErrStateNotFound) {
			return nil
		}

		return fmt.Errorf("load persisted cluster state for %q: %w", clusterName, err)
	}

	// Talos.ISO is a boot-time setting (Hetzner Cloud ISO ID) that cannot be
	// detected from the running cluster.
	if saved.Talos.ISO != 0 {
		spec.Talos.ISO = saved.Talos.ISO
	}

	// LocalRegistry configuration is not exposed by any cluster API.
	if saved.LocalRegistry.Registry != "" {
		spec.LocalRegistry = saved.LocalRegistry
	}

	// Vanilla.MirrorsDir is baked into the Kind node's containerd configuration at
	// creation; it cannot be read back from the running cluster.
	if saved.Vanilla.MirrorsDir != "" {
		spec.Vanilla.MirrorsDir = saved.Vanilla.MirrorsDir
	}

	// The AWS Load Balancer Controller opt-in is declarative KSail ownership
	// state. Preserve both true and false so enable and disable transitions are
	// each compared against the last successfully applied configuration.
	spec.EKS.ExperimentalAWSLoadBalancerController = saved.EKS.ExperimentalAWSLoadBalancerController

	return nil
}
