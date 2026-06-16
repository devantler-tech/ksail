package flux

import "k8s.io/apimachinery/pkg/runtime/schema"

// KustomizationGVR returns the GroupVersionResource for Flux Kustomizations.
func KustomizationGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "kustomize.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "kustomizations",
	}
}

// OCIRepositoryGVR returns the GroupVersionResource for Flux OCIRepositories.
func OCIRepositoryGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "ocirepositories",
	}
}

// HelmReleaseGVR returns the GroupVersionResource for Flux HelmReleases.
func HelmReleaseGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "helm.toolkit.fluxcd.io",
		Version:  "v2",
		Resource: "helmreleases",
	}
}
