package flux

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// sourceRef identifies the Flux source (OCIRepository / GitRepository / Bucket)
// referenced by a Kustomization's spec.sourceRef.
type sourceRef struct {
	Kind      string
	Name      string
	Namespace string
}

// parseKustomizationSourceRef extracts spec.sourceRef from a Kustomization CR.
// The namespace defaults to the Kustomization's own namespace when
// sourceRef.namespace is empty (Flux's default). Returns ok=false when no
// usable sourceRef is present.
func parseKustomizationSourceRef(kustomization *unstructured.Unstructured) (sourceRef, bool) {
	kind, _, _ := unstructured.NestedString(kustomization.Object, "spec", "sourceRef", "kind")
	name, _, _ := unstructured.NestedString(kustomization.Object, "spec", "sourceRef", "name")

	if kind == "" || name == "" {
		return sourceRef{}, false
	}

	namespace, _, _ := unstructured.NestedString(
		kustomization.Object, "spec", "sourceRef", "namespace",
	)
	if namespace == "" {
		namespace = kustomization.GetNamespace()
	}

	if namespace == "" {
		namespace = DefaultNamespace
	}

	return sourceRef{Kind: kind, Name: name, Namespace: namespace}, true
}

// sourceGVRForKind maps a Kustomization spec.sourceRef.kind to its
// GroupVersionResource. Returns ok=false for kinds that cannot back a
// Kustomization (e.g. HelmChart/HelmRepository).
func sourceGVRForKind(kind string) (schema.GroupVersionResource, bool) {
	switch kind {
	case "OCIRepository":
		return OCIRepositoryGVR(), true
	case "GitRepository":
		return GitRepositoryGVR(), true
	case "Bucket":
		return BucketGVR(), true
	default:
		return schema.GroupVersionResource{}, false
	}
}

// resolveSourceRevision returns the current artifact revision advertised by the
// Kustomization's source (status.artifact.revision) — the revision the source
// has just made available (e.g. after `ksail workload push`). It returns an
// empty string when the revision cannot be resolved (no sourceRef, unknown
// source kind, source missing, or no artifact yet); callers treat an empty
// revision as "revision-unaware" and fall back to condition-only readiness, so
// the readiness check never regresses when source state is unavailable.
func (r *Reconciler) resolveSourceRevision(
	ctx context.Context,
	kustomization *unstructured.Unstructured,
) string {
	ref, found := parseKustomizationSourceRef(kustomization)
	if !found {
		return ""
	}

	gvr, known := sourceGVRForKind(ref.Kind)
	if !known {
		return ""
	}

	source, err := r.Dynamic.Resource(gvr).
		Namespace(ref.Namespace).
		Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return ""
	}

	revision, _, _ := unstructured.NestedString(source.Object, "status", "artifact", "revision")

	return revision
}
