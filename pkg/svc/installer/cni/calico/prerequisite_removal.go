package calicoinstaller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/metadata"
)

func ownsLegacyStorage(object metav1.Object) bool {
	return object.GetLabels()["owner"] == helmStorageOwner &&
		object.GetLabels()["name"] == prerequisiteReleaseName &&
		!hasGitOpsOwner(object)
}

type prerequisiteRemoval struct {
	plan            *prerequisitePlan
	meta            metadata.Interface
	targets         map[string]prerequisiteRemovalTarget
	operatorHistory map[string]operatorHistoryIdentity
}

type prerequisiteRemovalTarget struct {
	resource        schema.GroupVersionResource
	resourceVersion string
}

func (r *prerequisiteRemoval) record(ctx context.Context) error {
	// Persist fully validated legacy identities before destruction so retries
	// cannot accept a same-name replacement after a partial uninstall.
	if len(r.plan.state.Resources) == 0 {
		return nil
	}

	return r.plan.save(ctx)
}

func (c *Installer) planPrerequisiteRemoval(ctx context.Context) (*prerequisiteRemoval, error) {
	plan, meta, err := c.loadPrerequisitePlan(ctx)
	if err != nil {
		return nil, err
	}

	operatorHistory, err := readOperatorHistory(ctx, meta)
	if err != nil {
		return nil, err
	}

	err = plan.captureLegacy(ctx, meta)
	if err != nil {
		return nil, err
	}

	removal := &prerequisiteRemoval{
		plan: plan, meta: meta, targets: make(map[string]prerequisiteRemovalTarget),
		operatorHistory: operatorHistory,
	}
	for _, ref := range plan.state.Resources {
		err = removal.inspectTarget(ctx, ref)
		if err != nil {
			return nil, err
		}
	}

	return removal, nil
}

func (r *prerequisiteRemoval) inspectTarget(ctx context.Context, ref prerequisiteRef) error {
	object, resource, err := r.observeTarget(ctx, ref)
	if apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect calico removal target %s: %w", ref.Name, err)
	}

	if object.Name != ref.Name || object.Namespace != ref.Namespace || object.UID != ref.UID {
		return prerequisiteError("Calico prerequisite %s identity changed", ref.Name)
	}

	owned := ownsPrerequisite(object)
	if ref.Storage {
		owned = ownsLegacyStorage(object)
	}

	if !owned {
		return prerequisiteError("Calico removal target %s ownership changed", ref.Name)
	}

	if object.ResourceVersion == "" {
		return prerequisiteError("Calico removal observation lacks resource version")
	}

	r.targets[ref.key()] = prerequisiteRemovalTarget{
		resource: resource, resourceVersion: object.ResourceVersion,
	}

	return nil
}

func (r *prerequisiteRemoval) observeTarget(
	ctx context.Context, ref prerequisiteRef,
) (*metav1.PartialObjectMetadata, schema.GroupVersionResource, error) {
	resource := ref.gvr()
	object, err := r.meta.Resource(resource).Namespace(ref.Namespace).
		Get(ctx, ref.Name, metav1.GetOptions{})

	if apierrors.IsNotFound(err) && resource.Group == admissionRegistrationGroup {
		// A removed serving version does not prove object absence. Only change
		// the version of this validated resource; inspection still fences the
		// recorded name, scope, UID, ownership and current resource version.
		resource.Version = "v1"
		if ref.Version == "v1" {
			resource.Version = admissionBetaVersion
		}

		object, err = r.meta.Resource(resource).Namespace(ref.Namespace).
			Get(ctx, ref.Name, metav1.GetOptions{})
	}

	if err != nil {
		return nil, resource, fmt.Errorf("observe Calico removal target: %w", err)
	}

	return object, resource, nil
}

func (r *prerequisiteRemoval) remove(ctx context.Context, timeout time.Duration) error {
	// Resource deletion finishes before the legacy history and inventory retire.
	for _, storage := range []bool{false, true} {
		for _, ref := range r.plan.state.Resources {
			if ref.Storage != storage {
				continue
			}

			err := r.removeTarget(ctx, ref, timeout)
			if err != nil {
				return err
			}
		}
	}

	if r.plan.inventory != nil {
		uid, version := r.plan.inventory.UID, r.plan.inventory.ResourceVersion

		err := r.plan.core.CoreV1().
			ConfigMaps(prerequisiteNamespace).
			Delete(ctx, prerequisiteInventory, metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version},
			})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("retire Calico inventory: %w", err)
		}
	}

	return nil
}

func (r *prerequisiteRemoval) removeTarget(
	ctx context.Context,
	ref prerequisiteRef,
	timeout time.Duration,
) error {
	target, observed := r.targets[ref.key()]
	if !observed {
		return nil
	}

	client := r.meta.Resource(target.resource).Namespace(ref.Namespace)

	err := client.Delete(ctx, ref.Name, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID: &ref.UID, ResourceVersion: &target.resourceVersion,
		},
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete calico prerequisite %s: %w", ref.Name, err)
	}

	ref.Version = target.resource.Version

	err = wait.PollUntilContextTimeout(ctx, prerequisitePollInterval, timeout, true,
		func(ctx context.Context) (bool, error) { return r.prerequisiteAbsent(ctx, ref) })
	if err != nil {
		return fmt.Errorf("wait for calico prerequisite %s absence: %w", ref.Name, err)
	}

	return nil
}

func (r *prerequisiteRemoval) prerequisiteAbsent(
	ctx context.Context,
	ref prerequisiteRef,
) (bool, error) {
	object, _, err := r.observeTarget(ctx, ref)
	if apierrors.IsNotFound(err) {
		return true, nil
	}

	if err != nil {
		return false, fmt.Errorf("read calico prerequisite absence: %w", err)
	}

	if object.Name != ref.Name || object.Namespace != ref.Namespace || object.UID != ref.UID {
		return false, prerequisiteError("Calico prerequisite replaced during removal")
	}

	return false, nil
}
