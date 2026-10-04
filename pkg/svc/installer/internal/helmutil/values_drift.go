package helmutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
)

// RenderedValues returns the user-supplied values Install sends to Helm for
// this chart, as Helm stores them on the release.
func (b *Base) RenderedValues() (map[string]any, error) {
	values, err := helm.UserSuppliedValues(b.spec)
	if err != nil {
		return nil, fmt.Errorf("%s: render values: %w", b.name, err)
	}

	return values, nil
}

// ValuesDrifted reports whether the installed release carries other
// user-supplied values than the ones this installer renders.
//
// `cluster update` reconciles a component when its ksail.yaml fields change.
// A KSail release that changes only the values it renders (ksail#7145's
// autoscaler CPU limit) produces no such change, so without this comparison it
// would never reach an existing cluster (ksail#7366).
//
// Three cases are not drift. A missing release is the spec field's job to
// install. A Flux- or Argo CD-owned release is skipped by Install, so drift
// would claim a reconcile that never happens. Empty rendered values cannot
// change a release, because Helm keeps the deployed values on an upgrade
// without any. A probe that cannot read the release returns an error and never
// reports "no drift".
func (b *Base) ValuesDrifted(ctx context.Context) (bool, error) {
	rendered, err := b.RenderedValues()
	if err != nil {
		return false, err
	}

	if len(rendered) == 0 {
		return false, nil
	}

	managed, err := b.ksailManagedReleaseExists(ctx)
	if err != nil || !managed {
		return false, err
	}

	deployed, err := b.client.GetReleaseValues(ctx, b.spec.ReleaseName, b.spec.Namespace)
	if err != nil {
		return false, fmt.Errorf("%s: read release values: %w", b.name, err)
	}

	deployedNormalized, err := normalizeValues(deployed)
	if err != nil {
		return false, fmt.Errorf("%s: normalize deployed values: %w", b.name, err)
	}

	renderedNormalized, err := normalizeValues(rendered)
	if err != nil {
		return false, fmt.Errorf("%s: normalize rendered values: %w", b.name, err)
	}

	return !reflect.DeepEqual(deployedNormalized, renderedNormalized), nil
}

// ksailManagedReleaseExists reports whether the release exists and is not owned
// by a GitOps controller, reading release storage labels the way Install does.
func (b *Base) ksailManagedReleaseExists(ctx context.Context) (bool, error) {
	exists, err := b.client.ReleaseExists(ctx, b.spec.ReleaseName, b.spec.Namespace)
	if err != nil {
		return false, fmt.Errorf("%s: check release: %w", b.name, err)
	}

	if !exists {
		return false, nil
	}

	labels, err := b.client.GetReleaseStorageLabels(ctx, b.spec.ReleaseName, b.spec.Namespace)
	if err != nil && !errors.Is(err, helm.ErrNoReleaseStorage) {
		return false, fmt.Errorf("%s: check release ownership: %w", b.name, err)
	}

	_, gitOpsManaged := IsGitOpsManaged(labels)

	return !gitOpsManaged, nil
}

// normalizeValues round-trips values through JSON so both sides compare with
// the same concrete types (float64 numbers, []any, map[string]any) whichever
// decoder produced them. Helm stores a release as JSON, so an int64 --set value
// reads back as float64. It also makes an empty map and a nil map compare equal.
func normalizeValues(values map[string]any) (map[string]any, error) {
	if len(values) == 0 {
		return map[string]any{}, nil
	}

	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode values: %w", err)
	}

	var normalized map[string]any

	err = json.Unmarshal(raw, &normalized)
	if err != nil {
		return nil, fmt.Errorf("decode values: %w", err)
	}

	return normalized, nil
}
