package calicoinstaller

import (
	"context"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func prerequisiteResources() []schema.GroupVersionResource {
	names := []string{
		"mutatingadmissionpolicies", "mutatingadmissionpolicybindings",
		"validatingadmissionpolicies", "validatingadmissionpolicybindings",
	}
	resources := make([]schema.GroupVersionResource, 0, len(names)+1)

	resources = append(resources, schema.GroupVersionResource{
		Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions",
	})
	for _, name := range names {
		resources = append(
			resources,
			schema.GroupVersionResource{
				Group:    "admissionregistration.k8s.io",
				Version:  "v1",
				Resource: name,
			},
		)
	}

	return resources
}

func (plan *prerequisitePlan) captureStorage(
	resource schema.GroupVersionResource,
	object metav1.Object,
) error {
	if !ownsLegacyStorage(object) {
		return prerequisiteError("legacy Calico storage ownership changed")
	}

	ref := refForObject(resource, object)

	ref.Storage = true

	err := validatePrerequisiteRef(ref)
	if err != nil {
		return err
	}

	err = recordedPrerequisiteIdentity(plan.state, ref)
	if err != nil {
		return err
	}

	rememberPrerequisite(&plan.state, ref)

	return nil
}

func (plan *prerequisitePlan) captureLegacyResource(
	ctx context.Context,
	resource schema.GroupVersionResource,
) error {
	objects, resource, err := plan.listLegacyResources(ctx, resource)
	if err != nil {
		return err
	}

	if objects == nil {
		return nil
	}

	if objects.GetContinue() != "" {
		return prerequisiteError("legacy Calico resource listing is incomplete")
	}

	for _, object := range objects.Items {
		err = plan.captureLegacyObject(ctx, resource, &object)
		if err != nil {
			return err
		}
	}

	return nil
}

func (plan *prerequisitePlan) captureLegacyObject(
	ctx context.Context,
	resource schema.GroupVersionResource,
	object metav1.Object,
) error {
	ref := refForObject(resource, object)
	if slices.ContainsFunc(plan.state.Dependencies, func(dependency prerequisiteDependency) bool {
		return dependency.key() == ref.key()
	}) {
		return plan.inspectListedDependency(ctx, resource, object)
	}

	annotations := object.GetAnnotations()
	if annotations["meta.helm.sh/release-name"] != prerequisiteReleaseName ||
		annotations["meta.helm.sh/release-namespace"] != prerequisiteNamespace {
		return nil
	}

	if !ownsPrerequisite(object) || object.GetUID() == "" {
		return prerequisiteError(
			"legacy Calico prerequisite %s has foreign or unknown ownership",
			object.GetName(),
		)
	}

	err := recordedPrerequisiteIdentity(plan.state, ref)
	if err != nil {
		return err
	}

	rememberPrerequisite(&plan.state, ref)

	return nil
}

func (plan *prerequisitePlan) inspectListedDependency(
	ctx context.Context,
	resource schema.GroupVersionResource,
	object metav1.Object,
) error {
	live, err := plan.client.Resource(resource).Get(ctx, object.GetName(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect recorded Calico dependency %s: %w", object.GetName(), err)
	}

	if object.GetUID() == "" || object.GetResourceVersion() == "" ||
		live.GetUID() != object.GetUID() || live.GetResourceVersion() != object.GetResourceVersion() {
		return prerequisiteError("listed Calico dependency identity changed")
	}
	// Revalidate recorded provenance and content without adopting the object.
	return plan.inspectDependency(ctx, live, live)
}

func (plan *prerequisitePlan) listLegacyResources(
	ctx context.Context,
	resource schema.GroupVersionResource,
) (
	*metav1.PartialObjectMetadataList, schema.GroupVersionResource, error,
) {
	if plan.meta == nil {
		return nil, resource, prerequisiteError("legacy Calico metadata client is unavailable")
	}

	objects, err := plan.meta.Resource(resource).List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) && resource.Group == "admissionregistration.k8s.io" {
		resource.Version = "v1beta1"

		objects, err = plan.meta.Resource(resource).List(ctx, metav1.ListOptions{})
		if apierrors.IsNotFound(err) {
			return nil, resource, nil
		}
	}

	if err != nil {
		return nil, resource, fmt.Errorf("inspect legacy Calico %s: %w", resource.Resource, err)
	}

	return objects, resource, nil
}
