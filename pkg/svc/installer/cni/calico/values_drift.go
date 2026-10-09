package calicoinstaller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

// ValuesDrifted detects a prerequisite migration or changed operator values
// even when the cluster configuration itself has not changed.
func (c *Installer) ValuesDrifted(ctx context.Context) (bool, error) {
	client, err := c.GetClient()
	if err != nil {
		return false, fmt.Errorf("calico drift client: %w", err)
	}

	exists, err := client.ReleaseExists(ctx, "calico", prerequisiteNamespace)
	if err != nil {
		return false, fmt.Errorf("inspect Calico release: %w", err)
	}

	skipped, err := c.prerequisitesGitOpsManaged(ctx)
	if err != nil || skipped {
		return false, err
	}

	if !exists {
		return c.recordedPrerequisites(ctx)
	}

	drifted, err := c.prerequisitesDrifted(ctx)
	if err != nil || drifted {
		return drifted, err
	}

	base := helmutil.NewBase("calico", client, c.GetTimeout(), nil, c.chartSpec())

	drifted, err = base.ValuesDrifted(ctx)
	if err != nil {
		return false, fmt.Errorf("calico operator values: %w", err)
	}

	return drifted, nil
}

func (c *Installer) recordedPrerequisites(ctx context.Context) (bool, error) {
	_, core, _, err := c.prerequisiteClients()
	if err != nil {
		return false, err
	}

	inventory, _, err := readPrerequisiteState(ctx, core)
	if err != nil {
		return false, err
	}

	return inventory != nil, nil
}

func (c *Installer) prerequisitesGitOpsManaged(ctx context.Context) (bool, error) {
	for _, release := range []string{"calico", prerequisiteReleaseName} {
		skipped, ownershipErr := c.CheckGitOpsOwnership(
			ctx,
			release,
			release,
			prerequisiteNamespace,
		)
		if ownershipErr != nil {
			return false, fmt.Errorf("inspect Calico ownership: %w", ownershipErr)
		}

		if skipped {
			return true, nil
		}
	}

	return false, nil
}

func (c *Installer) prerequisitesDrifted(ctx context.Context) (bool, error) {
	resources, core, _, err := c.prerequisiteClients()
	if err != nil {
		return false, fmt.Errorf("calico drift inventory client: %w", err)
	}

	inventory, state, err := readPrerequisiteState(ctx, core)
	if err != nil {
		return false, fmt.Errorf("calico drift inventory: %w", err)
	}

	if inventory == nil || !state.Complete || state.Version != chartVersion() {
		return true, nil
	}

	missing, err := prerequisitesMissing(ctx, resources, state)
	if err != nil {
		return false, fmt.Errorf("calico prerequisite drift: %w", err)
	}

	return missing, nil
}

func prerequisitesMissing(
	ctx context.Context,
	client dynamic.Interface,
	state prerequisiteState,
) (bool, error) {
	if len(state.Resources) == 0 {
		return true, nil
	}

	for _, ref := range state.Resources {
		if ref.Storage {
			continue
		}

		object, err := client.Resource(ref.gvr()).Get(ctx, ref.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		if err != nil {
			return false, fmt.Errorf("read prerequisite %s: %w", ref.Name, err)
		}

		if object.GetUID() != ref.UID || !ownsPrerequisite(object) {
			return false, prerequisiteError(
				"Calico prerequisite %s identity or ownership changed",
				ref.Name,
			)
		}
	}

	return false, nil
}
