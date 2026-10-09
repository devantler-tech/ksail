package calicoinstaller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const admissionRegistrationGroup = "admissionregistration.k8s.io"

// Dependencies are observed operator objects, never KSail's mutation/removal inventory.
type prerequisiteDependency struct {
	prerequisiteRef

	InstallationUID types.UID `json:"installationUid"`
	SpecSHA256      string    `json:"specSha256"`
}

func validateDependencyRef(ref prerequisiteDependency) error {
	err := validatePrerequisiteRef(ref.prerequisiteRef)
	if err != nil {
		return err
	}

	digest, err := hex.DecodeString(ref.SpecSHA256)
	if ref.Storage || ref.Group != admissionRegistrationGroup ||
		ref.InstallationUID == "" || err != nil || len(digest) != sha256.Size {
		return prerequisiteError("Calico operator dependency contains incomplete provenance")
	}

	return nil
}

func (plan *prerequisitePlan) isDependency(object *unstructured.Unstructured) bool {
	resource, err := prerequisiteResource(object)
	if err != nil {
		return false
	}

	key := refForObject(resource, object).key()

	return slices.ContainsFunc(plan.state.Dependencies, func(ref prerequisiteDependency) bool {
		return ref.key() == key
	})
}

func (plan *prerequisitePlan) inspectDependency(
	ctx context.Context, desired, live *unstructured.Unstructured,
) error {
	resource, err := prerequisiteResource(desired)
	if err != nil {
		return err
	}

	if desired.GroupVersionKind() != live.GroupVersionKind() || live.GetNamespace() != "" {
		return prerequisiteError("Calico operator prerequisite resource identity differs")
	}

	ref := refForObject(resource, live)

	if slices.ContainsFunc(plan.state.Resources, func(old prerequisiteRef) bool {
		return old.key() == ref.key()
	}) {
		return prerequisiteError("Calico owned prerequisite cannot become an external dependency")
	}

	installationUID, err := plan.operatorDependencyProvenance(ctx, live)
	if err != nil {
		return err
	}

	wanted, err := prerequisiteSpecDigest(desired)
	if err != nil {
		return err
	}

	actual, err := prerequisiteSpecDigest(live)
	if err != nil {
		return err
	}

	if wanted != actual {
		return prerequisiteError("Calico operator prerequisite content differs from the chart")
	}

	dependency := prerequisiteDependency{
		prerequisiteRef: ref, InstallationUID: installationUID, SpecSHA256: actual,
	}

	return plan.rememberDependency(dependency)
}

func (plan *prerequisitePlan) rememberDependency(dependency prerequisiteDependency) error {
	index := slices.IndexFunc(plan.state.Dependencies, func(old prerequisiteDependency) bool {
		return old.key() == dependency.key()
	})
	if index >= 0 {
		previous := plan.state.Dependencies[index]

		previous.Version = dependency.Version
		if previous != dependency {
			return prerequisiteError("Calico operator prerequisite identity or content changed")
		}

		plan.state.Dependencies[index] = dependency
	} else {
		plan.state.Dependencies = append(plan.state.Dependencies, dependency)
	}

	return nil
}

func (plan *prerequisitePlan) operatorDependencyProvenance(
	ctx context.Context, object *unstructured.Unstructured,
) (types.UID, error) {
	err := validateOperatorDependency(object)
	if err != nil {
		return "", err
	}

	installation, err := plan.client.Resource(schema.GroupVersionResource{
		Group: "operator.tigera.io", Version: "v1", Resource: "installations",
	}).Get(ctx, "default", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect Calico dependency Installation: %w", err)
	}

	err = validateDependencyInstallation(installation, object.GetOwnerReferences()[0].UID)
	if err != nil {
		return "", err
	}

	if plan.meta == nil {
		return "", prerequisiteError("Calico dependency history client is unavailable")
	}

	history, err := readOperatorHistory(ctx, plan.meta)
	if err != nil {
		return "", err
	}

	if len(history) == 0 {
		return "", prerequisiteError("Calico operator prerequisite lacks validated release history")
	}

	return installation.GetUID(), nil
}

func validateOperatorDependency(object *unstructured.Unstructured) error {
	if object.GetUID() == "" || object.GetResourceVersion() == "" ||
		object.GetDeletionTimestamp() != nil || hasGitOpsOwner(object) ||
		object.GroupVersionKind().Group != admissionRegistrationGroup {
		return prerequisiteError("Calico operator prerequisite has unverified provenance")
	}

	err := validateDependencyManagers(object)
	if err != nil {
		return err
	}

	return validateInstallationController(object)
}

func validateDependencyManagers(object *unstructured.Unstructured) error {
	label := "operator.tigera.io/validating-admission-policy"

	switch object.GetKind() {
	case "MutatingAdmissionPolicy", "MutatingAdmissionPolicyBinding":
		label = "operator.tigera.io/mutating-admission-policy"
	case "ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding":
	default:
		return prerequisiteError("Calico operator dependency is not an admission prerequisite")
	}

	if object.GetLabels()[label] != "managed" {
		return prerequisiteError("Calico operator prerequisite lacks its managed label")
	}

	for _, key := range []string{"meta.helm.sh/release-name", "meta.helm.sh/release-namespace"} {
		if _, present := object.GetAnnotations()[key]; present {
			return prerequisiteError(
				"Calico operator prerequisite has conflicting Helm ownership",
			)
		}
	}

	if _, present := object.GetLabels()[prerequisiteOwnerKey]; present {
		return prerequisiteError("Calico operator prerequisite has conflicting KSail ownership")
	}

	return nil
}

func validateInstallationController(object *unstructured.Unstructured) error {
	owners := object.GetOwnerReferences()
	if len(owners) != 1 || owners[0].APIVersion != "operator.tigera.io/v1" ||
		owners[0].Kind != "Installation" || owners[0].Name != "default" ||
		owners[0].Controller == nil || !*owners[0].Controller || owners[0].UID == "" {
		return prerequisiteError(
			"Calico operator prerequisite lacks its Installation controller",
		)
	}

	return nil
}

func validateDependencyInstallation(
	installation *unstructured.Unstructured,
	ownerUID types.UID,
) error {
	if installation.GetName() != "default" || installation.GetNamespace() != "" ||
		installation.GroupVersionKind() != (schema.GroupVersionKind{
			Group: "operator.tigera.io", Version: "v1", Kind: "Installation",
		}) || installation.GetUID() != ownerUID || installation.GetResourceVersion() == "" ||
		installation.GetDeletionTimestamp() != nil || !ownsDependencyInstallation(installation) {
		return prerequisiteError("Calico dependency Installation ownership is unverified")
	}

	return nil
}

func ownsDependencyInstallation(installation *unstructured.Unstructured) bool {
	annotations := installation.GetAnnotations()

	return !hasGitOpsOwner(installation) &&
		installation.GetLabels()["app.kubernetes.io/managed-by"] == "Helm" &&
		annotations["meta.helm.sh/release-name"] == calicoReleaseName &&
		annotations["meta.helm.sh/release-namespace"] == prerequisiteNamespace
}

func (plan *prerequisitePlan) complete(ctx context.Context) error {
	err := plan.revalidateDependencies(ctx)
	if err != nil {
		return err
	}

	plan.state.Complete = true

	return plan.save(ctx)
}

func (plan *prerequisitePlan) revalidateDependencies(ctx context.Context) error {
	for index, dependency := range plan.state.Dependencies {
		live, err := plan.observeDependency(ctx, dependency)
		if err != nil {
			return fmt.Errorf("observe Calico operator prerequisite: %w", err)
		}

		installationUID, err := plan.operatorDependencyProvenance(ctx, live)
		if err != nil {
			return err
		}

		digest, err := prerequisiteSpecDigest(live)
		if err != nil {
			return err
		}

		if live.GetUID() != dependency.UID || installationUID != dependency.InstallationUID ||
			digest != dependency.SpecSHA256 {
			return prerequisiteError("Calico operator prerequisite identity or content changed")
		}

		plan.state.Dependencies[index].Version = live.GroupVersionKind().Version
	}

	return nil
}

func (plan *prerequisitePlan) observeDependency(
	ctx context.Context, dependency prerequisiteDependency,
) (*unstructured.Unstructured, error) {
	resource := dependency.gvr()

	live, err := plan.client.Resource(resource).Get(ctx, dependency.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// Serving-version changes never authorize a new object incarnation.
		resource.Version = "v1"
		if dependency.Version == "v1" {
			resource.Version = admissionBetaVersion
		}

		live, err = plan.client.Resource(resource).Get(ctx, dependency.Name, metav1.GetOptions{})
	}

	if err != nil {
		return nil, fmt.Errorf("read Calico operator prerequisite: %w", err)
	}

	actual, err := prerequisiteResource(live)
	if err != nil {
		return nil, err
	}

	if actual != resource || live.GetName() != dependency.Name {
		return nil, prerequisiteError("Calico operator prerequisite resource identity differs")
	}

	return live, nil
}

func prerequisiteSpecDigest(object *unstructured.Unstructured) (string, error) {
	spec, found, err := unstructured.NestedMap(object.Object, "spec")
	if err != nil || !found {
		return "", prerequisiteError("Calico operator prerequisite lacks a valid spec")
	}
	// Normalize only Kubernetes admission-registration defaults; compare every other field.
	if strings.HasSuffix(object.GetKind(), "Policy") {
		if _, present := spec["failurePolicy"]; !present {
			spec["failurePolicy"] = "Fail"
		}
	}

	for _, key := range []string{"matchConstraints", "matchResources"} {
		match, exists := spec[key].(map[string]any)
		if !exists {
			continue
		}

		defaultAdmissionMatch(match)
	}

	data, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encode Calico operator prerequisite spec: %w", err)
	}

	digest := sha256.Sum256(data)

	return hex.EncodeToString(digest[:]), nil
}

func defaultAdmissionMatch(match map[string]any) {
	if _, present := match["matchPolicy"]; !present {
		match["matchPolicy"] = "Equivalent"
	}

	for _, selector := range []string{"namespaceSelector", "objectSelector"} {
		if _, present := match[selector]; !present {
			match[selector] = map[string]any{}
		}
	}

	for _, key := range []string{"resourceRules", "excludeResourceRules"} {
		rules, _ := match[key].([]any)
		for _, rule := range rules {
			if fields, ok := rule.(map[string]any); ok {
				if _, present := fields["scope"]; !present {
					fields["scope"] = "*"
				}
			}
		}
	}
}
