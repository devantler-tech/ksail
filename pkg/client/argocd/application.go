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
	// defaultSourcePath is "." because OCI artifacts contain manifests at root level.
	defaultSourcePath = "."

	argoCDRefreshAnnotationKey  = "argocd.argoproj.io/refresh"
	argoCDHardRefreshAnnotation = "hard"
)

func applicationGVR() schema.GroupVersionResource {
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
		sourcePath = defaultSourcePath
	}

	obj := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      name,
			"namespace": argoCDNamespace,
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
