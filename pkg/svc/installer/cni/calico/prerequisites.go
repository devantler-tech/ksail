package calicoinstaller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
)

const (
	prerequisiteNamespace    = "tigera-operator"
	prerequisiteInventory    = "ksail-calico-prerequisites"
	prerequisiteOwnerKey     = "ksail.io/component"
	prerequisiteOwner        = "calico-prerequisites"
	prerequisiteFieldManager = "ksail-calico-prerequisites"
	prerequisitePollInterval = 200 * time.Millisecond
	prerequisiteDecodeBuffer = 4096
	configMapResource        = "configmaps"
	secretResource           = "secrets"
)

var errInvalidPrerequisites = errors.New("invalid Calico prerequisites")

func prerequisiteError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidPrerequisites, fmt.Sprintf(format, args...))
}

type prerequisiteRef struct {
	Group     string    `json:"group"`
	Version   string    `json:"version"`
	Resource  string    `json:"resource"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace,omitempty"`
	UID       types.UID `json:"uid"`
	Storage   bool      `json:"storage,omitempty"`
}

func (r prerequisiteRef) gvr() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}
}

func (r prerequisiteRef) key() string {
	return r.Group + "/" + r.Resource + "/" + r.Namespace + "/" + r.Name
}

type prerequisiteState struct {
	Version   string            `json:"version"`
	Complete  bool              `json:"complete"`
	Resources []prerequisiteRef `json:"resources"`
}

type prerequisitePlan struct {
	client    dynamic.Interface
	core      kubernetes.Interface
	objects   []*unstructured.Unstructured
	state     prerequisiteState
	inventory *corev1.ConfigMap
}

func prerequisiteResource(object *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	gvk := object.GroupVersionKind()
	resource := prerequisiteResourceForKind(gvk)

	if resource == "" || object.GetName() == "" || object.GetNamespace() != "" ||
		object.GetGenerateName() != "" {
		return schema.GroupVersionResource{}, prerequisiteError(
			"unexpected Calico prerequisite %s %q",
			gvk,
			object.GetName(),
		)
	}

	return schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: resource,
	}, nil
}

func renderedPrerequisiteResource(
	object *unstructured.Unstructured,
) (schema.GroupVersionResource, error) {
	if object.GetUID() != "" || object.GetResourceVersion() != "" {
		return schema.GroupVersionResource{}, prerequisiteError(
			"rendered chart contains unverified live identity",
		)
	}

	return prerequisiteResource(object)
}

func prerequisiteResourceForKind(gvk schema.GroupVersionKind) string {
	if gvk.Group == "apiextensions.k8s.io" && gvk.Version == "v1" &&
		gvk.Kind == "CustomResourceDefinition" {
		return "customresourcedefinitions"
	}

	if gvk.Group != "admissionregistration.k8s.io" ||
		(gvk.Version != "v1" && gvk.Version != "v1beta1") {
		return ""
	}

	return map[string]string{
		"MutatingAdmissionPolicy":          "mutatingadmissionpolicies",
		"MutatingAdmissionPolicyBinding":   "mutatingadmissionpolicybindings",
		"ValidatingAdmissionPolicy":        "validatingadmissionpolicies",
		"ValidatingAdmissionPolicyBinding": "validatingadmissionpolicybindings",
	}[gvk.Kind]
}

func parsePrerequisites(manifest string) ([]*unstructured.Unstructured, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), prerequisiteDecodeBuffer)
	objects := make([]*unstructured.Unstructured, 0)
	seen := make(map[string]bool)
	crds := 0

	for {
		object := new(unstructured.Unstructured)

		err := decoder.Decode(object)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("decode Calico prerequisites: %w", err)
		}

		if len(object.Object) == 0 {
			continue
		}

		gvr, err := renderedPrerequisiteResource(object)
		if err != nil {
			return nil, err
		}

		key := gvr.Group + "/" + gvr.Resource + "/" + object.GetName()
		if seen[key] {
			return nil, prerequisiteError("duplicate Calico prerequisite %s", key)
		}

		seen[key] = true

		if gvr.Resource == "customresourcedefinitions" {
			crds++
		}

		objects = append(objects, object)
	}

	if crds == 0 {
		return nil, prerequisiteError("Calico prerequisite chart contains no CRDs")
	}

	return objects, nil
}

func hasGitOpsOwner(object metav1.Object) bool {
	labels := object.GetLabels()
	annotations := object.GetAnnotations()

	if _, managed := helmutil.IsGitOpsManaged(labels); managed {
		return true
	}

	for _, key := range []string{
		"helm.toolkit.fluxcd.io/namespace", "kustomize.toolkit.fluxcd.io/name",
		"kustomize.toolkit.fluxcd.io/namespace", "argocd.argoproj.io/instance",
	} {
		if _, present := labels[key]; present {
			return true
		}
	}

	_, present := annotations["argocd.argoproj.io/tracking-id"]

	return present
}

func ownsPrerequisite(object metav1.Object) bool {
	if hasGitOpsOwner(object) {
		return false
	}

	labels, annotations := object.GetLabels(), object.GetAnnotations()
	_, hasRelease := annotations["meta.helm.sh/release-name"]

	_, hasNamespace := annotations["meta.helm.sh/release-namespace"]
	if (hasRelease || hasNamespace) &&
		(annotations["meta.helm.sh/release-name"] != "calico-crds" ||
			annotations["meta.helm.sh/release-namespace"] != prerequisiteNamespace) {
		return false
	}

	return labels[prerequisiteOwnerKey] == prerequisiteOwner ||
		(labels["app.kubernetes.io/managed-by"] == "Helm" &&
			annotations["meta.helm.sh/release-name"] == "calico-crds" &&
			annotations["meta.helm.sh/release-namespace"] == prerequisiteNamespace)
}

func (c *Installer) prerequisiteClients() (
	dynamic.Interface, kubernetes.Interface, metadata.Interface, error,
) {
	config, err := c.BuildRESTConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("calico prerequisites configuration: %w", err)
	}

	config.ContentType = "application/json"
	config.AcceptContentTypes = "application/json"

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("calico resource client: %w", err)
	}

	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("calico inventory client: %w", err)
	}

	meta, err := metadata.NewForConfig(config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("calico metadata client: %w", err)
	}

	return client, core, meta, nil
}

func readPrerequisiteState(
	ctx context.Context,
	core kubernetes.Interface,
) (*corev1.ConfigMap, prerequisiteState, error) {
	object, err := core.CoreV1().
		ConfigMaps(prerequisiteNamespace).
		Get(ctx, prerequisiteInventory, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, prerequisiteState{}, nil
	}

	if err != nil {
		return nil, prerequisiteState{}, fmt.Errorf("read Calico inventory: %w", err)
	}

	if object.Labels[prerequisiteOwnerKey] != prerequisiteOwner || !ownsPrerequisite(object) ||
		object.UID == "" {
		return nil, prerequisiteState{}, prerequisiteError("Calico inventory is not owned by KSail")
	}

	var state prerequisiteState

	err = json.Unmarshal([]byte(object.Data["inventory.json"]), &state)
	if err != nil {
		return nil, state, fmt.Errorf("decode Calico inventory: %w", err)
	}

	seen := make(map[string]bool)

	for _, ref := range state.Resources {
		err = validatePrerequisiteRef(ref)
		if err != nil {
			return nil, state, err
		}

		if seen[ref.key()] {
			return nil, state, prerequisiteError("Calico inventory contains duplicate identities")
		}

		seen[ref.key()] = true
	}

	return object, state, nil
}

func validatePrerequisiteRef(ref prerequisiteRef) error {
	if ref.Name == "" || ref.UID == "" {
		return prerequisiteError("Calico inventory contains incomplete identity")
	}

	if ref.Storage {
		return validatePrerequisiteStorage(ref)
	}

	if ref.Namespace != "" || ref.Version == "" {
		return prerequisiteError("Calico inventory contains invalid scope")
	}

	for _, kind := range []string{
		"CustomResourceDefinition", "MutatingAdmissionPolicy", "MutatingAdmissionPolicyBinding",
		"ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding",
	} {
		object := &unstructured.Unstructured{}
		object.SetGroupVersionKind(
			schema.GroupVersionKind{Group: ref.Group, Version: ref.Version, Kind: kind},
		)
		object.SetName(ref.Name)

		gvr, err := prerequisiteResource(object)
		if err == nil && gvr == ref.gvr() {
			return nil
		}
	}

	return prerequisiteError("Calico inventory contains an unexpected resource")
}

func validatePrerequisiteStorage(ref prerequisiteRef) error {
	if ref.Group != "" || ref.Version != "v1" || ref.Namespace != prerequisiteNamespace ||
		(ref.Resource != "secrets" && ref.Resource != configMapResource) ||
		!strings.HasPrefix(ref.Name, "sh.helm.release.v1.calico-crds.v") {
		return prerequisiteError("Calico inventory contains unrelated Helm storage")
	}

	return nil
}

func rememberPrerequisite(state *prerequisiteState, ref prerequisiteRef) {
	index := slices.IndexFunc(state.Resources, func(old prerequisiteRef) bool {
		return old.Group == ref.Group && old.Resource == ref.Resource &&
			old.Namespace == ref.Namespace &&
			old.Name == ref.Name
	})
	if index < 0 {
		state.Resources = append(state.Resources, ref)
	} else {
		state.Resources[index] = ref
	}
}

func recordedPrerequisiteIdentity(state prerequisiteState, ref prerequisiteRef) error {
	for _, old := range state.Resources {
		if old.Group == ref.Group && old.Resource == ref.Resource &&
			old.Namespace == ref.Namespace &&
			old.Name == ref.Name &&
			old.UID != ref.UID {
			return prerequisiteError("Calico prerequisite %s identity changed", ref.Name)
		}
	}

	return nil
}

func (plan *prerequisitePlan) save(ctx context.Context) error {
	data, err := json.Marshal(plan.state)
	if err != nil {
		return fmt.Errorf("encode Calico inventory: %w", err)
	}

	object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: prerequisiteInventory, Namespace: prerequisiteNamespace,
		Labels: map[string]string{prerequisiteOwnerKey: prerequisiteOwner},
	}, Data: map[string]string{"inventory.json": string(data)}}
	if plan.inventory == nil {
		plan.inventory, err = plan.core.CoreV1().
			ConfigMaps(prerequisiteNamespace).
			Create(ctx, object, metav1.CreateOptions{})
	} else {
		previous := plan.inventory
		if previous.ResourceVersion == "" {
			return prerequisiteError("Calico inventory observation lacks resource version")
		}

		object.Labels = previous.DeepCopy().Labels
		object.Annotations = previous.DeepCopy().Annotations
		object.UID = plan.inventory.UID
		object.ResourceVersion = plan.inventory.ResourceVersion

		plan.inventory, err = plan.core.CoreV1().
			ConfigMaps(prerequisiteNamespace).
			Update(ctx, object, metav1.UpdateOptions{})
		if err == nil && plan.inventory.UID != previous.UID {
			return prerequisiteError("Calico inventory identity changed during update")
		}
	}

	if err != nil {
		return fmt.Errorf("persist Calico inventory: %w", err)
	}

	if plan.inventory.UID == "" || plan.inventory.ResourceVersion == "" {
		return prerequisiteError("Calico inventory write lacks identity")
	}

	return nil
}

func (c *Installer) loadPrerequisitePlan(
	ctx context.Context,
) (*prerequisitePlan, metadata.Interface, error) {
	client, core, meta, err := c.prerequisiteClients()
	if err != nil {
		return nil, nil, err
	}

	inventory, state, err := readPrerequisiteState(ctx, core)
	if err != nil {
		return nil, nil, err
	}

	return &prerequisitePlan{
		client:    client,
		core:      core,
		inventory: inventory,
		state:     state,
	}, meta, nil
}

func (c *Installer) planPrerequisites(
	ctx context.Context,
	manifest string,
) (*prerequisitePlan, error) {
	objects, err := parsePrerequisites(manifest)
	if err != nil {
		return nil, err
	}

	plan, meta, err := c.loadPrerequisitePlan(ctx)
	if err != nil {
		return nil, err
	}

	plan.objects = objects
	// Validate the entire bundle before any namespace, inventory or object write.
	for _, object := range objects {
		err = plan.inspectTarget(ctx, object)
		if err != nil {
			return nil, err
		}
	}

	err = plan.captureLegacy(ctx, meta)
	if err != nil {
		return nil, err
	}

	return plan, nil
}

func (plan *prerequisitePlan) captureLegacy(ctx context.Context, meta metadata.Interface) error {
	gvr, err := helmStorageResource()
	if err != nil {
		return err
	}

	err = validatePrerequisiteReleaseStorage(ctx, meta, gvr)
	if err != nil {
		return err
	}

	list, err := meta.Resource(gvr).Namespace(prerequisiteNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: "owner=helm,name=calico-crds"})
	if err != nil {
		return fmt.Errorf("inspect legacy Calico release storage: %w", err)
	}

	if list.GetContinue() != "" {
		return prerequisiteError("legacy Calico storage listing is incomplete")
	}

	for _, object := range list.Items {
		err = plan.captureStorage(gvr, &object)
		if err != nil {
			return err
		}
	}

	if len(list.Items) == 0 {
		return nil
	}

	for _, resource := range prerequisiteResources() {
		err = plan.captureLegacyResource(ctx, resource)
		if err != nil {
			return err
		}
	}

	return nil
}

func (plan *prerequisitePlan) apply(ctx context.Context) error {
	err := plan.ensureNamespace(ctx)
	if err != nil {
		return err
	}

	plan.state.Version, plan.state.Complete = chartVersion(), false

	err = plan.save(ctx)
	if err != nil {
		return err
	}

	for _, object := range plan.objects {
		err = plan.applyObject(ctx, object)
		if err != nil {
			return err
		}

		err = plan.save(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (plan *prerequisitePlan) established(ctx context.Context, timeout time.Duration) error {
	for _, object := range plan.objects {
		if object.GetKind() != "CustomResourceDefinition" {
			continue
		}

		err := plan.waitEstablished(ctx, object, timeout)
		if err != nil {
			return err
		}
	}

	return nil
}
