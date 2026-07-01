package flux

import "k8s.io/apimachinery/pkg/runtime/schema"

// sourceAPIGroup is the API group shared by all Flux source kinds
// (OCIRepository, GitRepository, Bucket).
const sourceAPIGroup = "source.toolkit.fluxcd.io"

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
		Group:    sourceAPIGroup,
		Version:  "v1",
		Resource: "ocirepositories",
	}
}

// GitRepositoryGVR returns the GroupVersionResource for Flux GitRepositories.
func GitRepositoryGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    sourceAPIGroup,
		Version:  "v1",
		Resource: "gitrepositories",
	}
}

// BucketGVR returns the GroupVersionResource for Flux Buckets.
func BucketGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    sourceAPIGroup,
		Version:  "v1",
		Resource: "buckets",
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
