package argocd

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	defaultApplicationName      = "ksail"
	defaultDestinationNamespace = "default"
	defaultDestinationServer    = "https://kubernetes.default.svc"
	defaultProject              = "default"

	argoCDRefreshAnnotationKey  = "argocd.argoproj.io/refresh"
	argoCDHardRefreshAnnotation = "hard"
)

// DefaultSourcePath is the path the generated root Application points at inside
// the OCI artifact. Argo CD resolves it relative to the root of the expanded
// archive, so "." selects the archive root.
//
// The workload artifact builder (pkg/client/oci) must publish manifests at this
// path. Both sides are pinned by TestManifestLayerServesArgoCDSourcePath; see
// https://github.com/devantler-tech/ksail/issues/6284 for the outage that
// followed when they disagreed.
const DefaultSourcePath = "."

// ApplicationGVR returns the GroupVersionResource for ArgoCD Applications.
func ApplicationGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
}

func buildApplication(opts EnsureOptions) *unstructured.Unstructured {
	name := opts.ApplicationName
	if name == "" {
		name = defaultApplicationName
	}

	sourcePath := opts.SourcePath
	if sourcePath == "" {
		sourcePath = DefaultSourcePath
	}

	obj := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      name,
			"namespace": DefaultNamespace,
		},
		"spec": map[string]any{
			"project": defaultProject,
			"source": map[string]any{
				"repoURL":        opts.RepositoryURL,
				"targetRevision": opts.TargetRevision,
				"path":           sourcePath,
			},
			"destination": map[string]any{
				"server":    defaultDestinationServer,
				"namespace": defaultDestinationNamespace,
			},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"prune": true, "selfHeal": true},
				"syncOptions": []any{"CreateNamespace=true"},
			},
		},
	}

	return &unstructured.Unstructured{Object: obj}
}
