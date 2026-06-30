package environment

import (
	"errors"
	"fmt"
	"path"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// ClustersDir is the conventional sub-directory under a GitOps source directory
// that holds the per-environment cluster overlays (<sourceDir>/clusters/<env>/),
// alongside the shared base at <sourceDir>/clusters/base/. The cluster
// add-environment command clones from <sourceDir>/clusters/<from>/, so a
// multi-cluster scaffold seeds exactly this layout.
const ClustersDir = "clusters"

// BaseEnvName is the conventional name of the shared base overlay in a
// multi-cluster source tree (clusters/base/). Per-environment overlays sit
// alongside it at clusters/<env>/ and reference it, so cross-environment wiring
// lives in one place instead of being copied into each overlay.
const BaseEnvName = "base"

// kustomizationFileName is the standard kustomize manifest filename, matching the
// scaffolder's single-cluster output so the two layouts stay byte-consistent.
const kustomizationFileName = "kustomization.yaml"

// ErrReservedEnvironmentName is returned by DeriveMultiClusterLayout when the
// environment name collides with the shared base directory (clusters/base/),
// which would make the per-environment overlay overwrite the base.
var ErrReservedEnvironmentName = errors.New("environment name is reserved")

// ErrEmptyEnvironmentName is returned by DeriveMultiClusterLayout when the
// environment name is empty: unlike a cluster config, a multi-cluster overlay has
// no "default" environment, and an empty name would collapse the clusters/<env>/
// path.
var ErrEmptyEnvironmentName = errors.New("environment name must be non-empty")

// LayoutFile is one file in a scaffolded multi-cluster overlay tree: the path
// relative to the GitOps source directory and the kustomization model to render
// there. The model is rendered by the same kustomization generator the scaffolder
// uses for the single-cluster layout, so a multi-cluster scaffold stays
// byte-consistent with a single-cluster one and a later increment can write these
// files without bespoke YAML.
type LayoutFile struct {
	// RelPath is the file's path relative to the source directory, slash-delimited
	// (e.g. "clusters/base/kustomization.yaml"). The writer joins it onto the
	// resolved source directory.
	RelPath string
	// Kustomization is the model to render at RelPath.
	Kustomization *ktypes.Kustomization
}

// DeriveMultiClusterLayout returns the files for a multi-cluster GitOps source
// tree: a shared base at clusters/base/ plus one per-environment overlay at
// clusters/<envName>/ that references the base. It is the pure, filesystem-free
// foundation of the multi-cluster mode for cluster init — the structured
// counterpart to [DeriveRewrites] for the clone path — so the layout can be
// asserted in isolation before a later increment threads it through the
// scaffolder and an init flag.
//
// The base kustomization starts with no resources, matching the single-cluster
// scaffold (KSail creates the GitOps resources — FluxInstance, ArgoCD Application
// — server-side, not in the source tree); the user fills clusters/base/ with the
// shared workload. The environment overlay references the base via the sibling
// path ../base, so adding a second environment is a [CloneOverlay] of an existing
// clusters/<env>/ overlay — the layout the cluster add-environment command
// already expects.
//
// envName is validated with [v1alpha1.ValidateClusterName] (defence in depth
// ahead of any downstream containment guard) and must not be the reserved base
// name, which would collide the overlay with the shared base. An empty name is
// rejected explicitly because ValidateClusterName treats "" as "use the default"
// (it returns nil), but an empty overlay name has no meaning here and would
// collapse the clusters/<env>/ path.
func DeriveMultiClusterLayout(envName string) ([]LayoutFile, error) {
	if envName == "" {
		return nil, fmt.Errorf("invalid environment name: %w", ErrEmptyEnvironmentName)
	}

	err := v1alpha1.ValidateClusterName(envName)
	if err != nil {
		return nil, fmt.Errorf("invalid environment name: %w", err)
	}

	if envName == BaseEnvName {
		return nil, fmt.Errorf(
			"%w: %q names the shared base overlay (clusters/%s/)",
			ErrReservedEnvironmentName, envName, BaseEnvName,
		)
	}

	// The overlay sits at clusters/<env>/ and the base at clusters/base/, so the
	// overlay references the base by stepping up one level to its sibling.
	baseRef := path.Join("..", BaseEnvName)

	return []LayoutFile{
		{
			RelPath:       path.Join(ClustersDir, BaseEnvName, kustomizationFileName),
			Kustomization: &ktypes.Kustomization{},
		},
		{
			RelPath: path.Join(ClustersDir, envName, kustomizationFileName),
			Kustomization: &ktypes.Kustomization{
				Resources: []string{baseRef},
			},
		},
	}, nil
}
