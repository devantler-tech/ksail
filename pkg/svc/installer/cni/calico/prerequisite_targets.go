package calicoinstaller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func refForObject(resource schema.GroupVersionResource, object metav1.Object) prerequisiteRef {
	return prerequisiteRef{
		Group: resource.Group, Version: resource.Version, Resource: resource.Resource,
		Name: object.GetName(), Namespace: object.GetNamespace(), UID: object.GetUID(),
	}
}

func (plan *prerequisitePlan) inspectTarget(
	ctx context.Context,
	object *unstructured.Unstructured,
) error {
	resource, err := prerequisiteResource(object)
	if err != nil {
		return err
	}

	live, err := plan.client.Resource(resource).Get(ctx, object.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect Calico prerequisite %s: %w", object.GetName(), err)
	}

	if live.GetUID() == "" || !ownsPrerequisite(live) {
		return prerequisiteError(
			"Calico prerequisite %s has foreign or unknown ownership",
			object.GetName(),
		)
	}

	if live.GetResourceVersion() == "" {
		return prerequisiteError("Calico prerequisite lacks resource version")
	}

	err = recordedPrerequisiteIdentity(plan.state, refForObject(resource, live))
	if err != nil {
		return err
	}

	object.SetUID(live.GetUID())
	object.SetResourceVersion(live.GetResourceVersion())

	return nil
}

func (plan *prerequisitePlan) ensureNamespace(ctx context.Context) error {
	_, err := plan.core.CoreV1().Namespaces().Get(ctx, prerequisiteNamespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = plan.core.CoreV1().Namespaces().Create(ctx,
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: prerequisiteNamespace},
			}, metav1.CreateOptions{})
	}

	if err != nil {
		return fmt.Errorf("ensure Calico namespace: %w", err)
	}

	return nil
}

func (plan *prerequisitePlan) applyObject(
	ctx context.Context,
	object *unstructured.Unstructured,
) error {
	resource, err := prerequisiteResource(object)
	if err != nil {
		return err
	}

	labels := object.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[prerequisiteOwnerKey] = prerequisiteOwner
	object.SetLabels(labels)

	data, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("encode Calico prerequisite: %w", err)
	}

	var live *unstructured.Unstructured
	if object.GetUID() == "" {
		live, err = plan.client.Resource(resource).Create(ctx, object,
			metav1.CreateOptions{FieldManager: prerequisiteFieldManager})
	} else {
		// UID and resourceVersion fence replacement and ownership transfer.
		force := true
		live, err = plan.client.Resource(resource).
			Patch(ctx, object.GetName(), types.ApplyPatchType, data,
				metav1.PatchOptions{FieldManager: prerequisiteFieldManager, Force: &force})
	}

	if err != nil {
		return fmt.Errorf("apply Calico prerequisite %s: %w", object.GetName(), err)
	}

	if live.GetUID() == "" {
		return prerequisiteError("Calico prerequisite apply lacks identity")
	}

	if live.GetName() != object.GetName() ||
		(object.GetUID() != "" && live.GetUID() != object.GetUID()) {
		return prerequisiteError(
			"Calico prerequisite %s identity changed during apply",
			object.GetName(),
		)
	}

	rememberPrerequisite(&plan.state, refForObject(resource, live))

	return nil
}

func (plan *prerequisitePlan) waitEstablished(
	ctx context.Context,
	object *unstructured.Unstructured,
	timeout time.Duration,
) error {
	resource, err := prerequisiteResource(object)
	if err != nil {
		return err
	}

	index := slices.IndexFunc(plan.state.Resources, func(ref prerequisiteRef) bool {
		return ref.Group == resource.Group && ref.Resource == resource.Resource &&
			ref.Name == object.GetName()
	})
	if index < 0 {
		return prerequisiteError("Calico CRD establishment lacks recorded identity")
	}

	ref := plan.state.Resources[index]

	err = wait.PollUntilContextTimeout(
		ctx,
		prerequisitePollInterval,
		timeout,
		true,
		func(ctx context.Context) (bool, error) {
			live, err := plan.client.Resource(resource).Get(ctx, ref.Name, metav1.GetOptions{})
			if err != nil {
				return false, fmt.Errorf("observe Calico CRD: %w", err)
			}

			if live.GetUID() != ref.UID {
				return false, prerequisiteError("Calico CRD identity changed during establishment")
			}

			return isCRDEstablished(live), nil
		},
	)
	if err != nil {
		return fmt.Errorf("wait for Calico CRD %s establishment: %w", ref.Name, err)
	}

	return nil
}

func isCRDEstablished(object *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, condition := range conditions {
		value, ok := condition.(map[string]any)
		if ok && value["type"] == "Established" && value["status"] == "True" {
			return true
		}
	}

	return false
}
