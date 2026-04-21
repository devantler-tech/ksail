package registryresolver

import (
	"context"
	"errors"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrNoLocalRegistry is returned when no local registry container is found.
var ErrNoLocalRegistry = errors.New(
	"no running local registry found; " +
		"create a cluster with '--local-registry' during cluster init",
)

// ErrNoGitOpsEngine is returned when no GitOps engine is detected.
var ErrNoGitOpsEngine = errors.New(
	"no GitOps engine detected in cluster; " +
		"create a cluster with '--gitops-engine Flux|ArgoCD' during cluster init",
)

// DetectGitOpsEngine checks if Flux or ArgoCD is deployed in the cluster.
func DetectGitOpsEngine(ctx context.Context) (v1alpha1.GitOpsEngine, error) {
	clientset, err := getKubernetesClient()
	if err != nil {
		// If we can't get cluster config, we can't detect GitOps engine.
		// Return ErrNoGitOpsEngine so the user knows detection failed.
		return v1alpha1.GitOpsEngineNone, ErrNoGitOpsEngine
	}

	// Check for Flux (flux-system namespace)
	_, err = clientset.CoreV1().Namespaces().Get(ctx, "flux-system", metav1.GetOptions{})
	if err == nil {
		return v1alpha1.GitOpsEngineFlux, nil
	}

	// Check for ArgoCD (argocd namespace)
	_, err = clientset.CoreV1().Namespaces().Get(ctx, "argocd", metav1.GetOptions{})
	if err == nil {
		return v1alpha1.GitOpsEngineArgoCD, nil
	}

	return v1alpha1.GitOpsEngineNone, ErrNoGitOpsEngine
}
