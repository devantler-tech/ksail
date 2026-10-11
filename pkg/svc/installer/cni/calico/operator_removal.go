package calicoinstaller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/metadata"
)

var errOperatorHistoryChanged = errors.New(
	"calico operator history changed during removal preflight",
)

const calicoReleaseName = "calico"

type operatorHistoryIdentity struct {
	UID     types.UID
	Version string
}

func helmStorageResource() (schema.GroupVersionResource, error) {
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("HELM_DRIVER")))

	err := helm.ValidateKubernetesReleaseStorageDriver(driver)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("calico release storage: %w", err)
	}

	resource := secretResource
	if driver == "configmap" || driver == configMapResource {
		resource = configMapResource
	}

	return schema.GroupVersionResource{Version: "v1", Resource: resource}, nil
}

func readOperatorHistory(
	ctx context.Context,
	client metadata.Interface,
) (map[string]operatorHistoryIdentity, error) {
	resource, err := helmStorageResource()
	if err != nil {
		return nil, err
	}

	err = rejectOtherReleaseStorage(ctx, client, resource, calicoReleaseName)
	if err != nil {
		return nil, err
	}

	objects, err := client.Resource(resource).Namespace(prerequisiteNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: "owner=helm,name=calico"})
	if err != nil {
		return nil, fmt.Errorf("read Calico operator history: %w", err)
	}

	if objects.GetContinue() != "" {
		return nil, errOperatorHistoryChanged
	}

	history := make(map[string]operatorHistoryIdentity, len(objects.Items))
	for _, object := range objects.Items {
		if !validOperatorHistory(&object) {
			return nil, errOperatorHistoryChanged
		}

		history[object.Name] = operatorHistoryIdentity{
			UID:     object.UID,
			Version: object.ResourceVersion,
		}
	}

	return history, nil
}

func validOperatorHistory(object metav1.Object) bool {
	return strings.HasPrefix(object.GetName(), "sh.helm.release.v1.calico.v") &&
		object.GetUID() != "" && object.GetResourceVersion() != "" &&
		object.GetLabels()["owner"] == helmStorageOwner && object.GetLabels()["name"] == calicoReleaseName &&
		!hasGitOpsOwner(object)
}

func (r *prerequisiteRemoval) removeOperator(ctx context.Context, client helm.Interface) error {
	current, err := readOperatorHistory(ctx, r.meta)
	if err != nil {
		return err
	}

	if !maps.Equal(current, r.operatorHistory) {
		return errOperatorHistoryChanged
	}
	// Empty history means confirmed absence, including after an interrupted
	// uninstall. Failed or pending revisions remain present and are uninstalled.
	if len(current) == 0 {
		return nil
	}

	err = client.UninstallRelease(ctx, calicoReleaseName, prerequisiteNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall calico release: %w", err)
	}

	return nil
}

func rejectOtherReleaseStorage(
	ctx context.Context,
	client metadata.Interface,
	resource schema.GroupVersionResource,
	release string,
) error {
	other := schema.GroupVersionResource{Version: "v1", Resource: secretResource}
	if resource.Resource == secretResource {
		other.Resource = configMapResource
	}

	objects, err := client.Resource(other).Namespace(prerequisiteNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: "owner=helm,name=" + release})
	if err != nil {
		return fmt.Errorf("inspect other calico release storage backend: %w", err)
	}

	if len(objects.Items) > 0 || objects.GetContinue() != "" {
		return prerequisiteError("Calico release storage backend does not match existing history")
	}

	return nil
}

func validatePrerequisiteReleaseStorage(
	ctx context.Context,
	client metadata.Interface,
	resource schema.GroupVersionResource,
) error {
	for _, release := range []string{calicoReleaseName, prerequisiteReleaseName} {
		err := rejectOtherReleaseStorage(ctx, client, resource, release)
		if err != nil {
			return err
		}
	}

	return nil
}
