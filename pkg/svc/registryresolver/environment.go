package registryresolver

import (
	"context"
	"errors"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ErrNoLocalRegistry is returned when no local registry container is found.
var ErrNoLocalRegistry = errors.New(
	"no running local registry found; " +
		"create a cluster with '--local-registry' during project init",
)

// ErrNoGitOpsEngine is returned when no GitOps engine is detected.
var ErrNoGitOpsEngine = errors.New(
	"no GitOps engine detected in cluster; " +
		"create a cluster with '--gitops-engine Flux|ArgoCD' during project init",
)

// DetectGitOpsEngine reports which GitOps engine is deployed in the cluster
// targeted by the given Clients bundle.
//
// Detection is layered so the two historical signals agree:
//  1. Primary — the Helm-release detector (detector.DetectGitOpsEngine), which
//     is the same mechanism the update reconciler uses to build its baseline.
//  2. Secondary — a namespace probe (flux-system / argocd), used when no
//     KSail-managed Helm release is found or the Helm history is unreadable
//     (e.g. restricted RBAC). This keeps non-KSail installs such as a plain
//     `flux bootstrap` cluster — which creates flux-system with no
//     flux-operator release — detecting as Flux instead of regressing to None.
func DetectGitOpsEngine(
	ctx context.Context,
	clients *Clients,
) (v1alpha1.GitOpsEngine, error) {
	helmClient, helmErr := clients.helmClient()
	if helmErr == nil {
		engine, detectErr := detector.DetectGitOpsEngine(ctx, helmClient)
		if detectErr == nil && engine != v1alpha1.GitOpsEngineNone {
			return engine, nil
		}
	}

	// Secondary: probe for the engine namespaces. This runs both when the Helm
	// history is unreadable and when no KSail-managed release was found, so that
	// non-KSail-managed engines are still classified correctly.
	return detectGitOpsEngineByNamespace(ctx, clients)
}

// detectGitOpsEngineByNamespace probes for the flux-system / argocd namespaces
// as a secondary GitOps-engine signal. Returns ErrNoGitOpsEngine when neither
// namespace exists or the cluster is unreachable.
func detectGitOpsEngineByNamespace(
	ctx context.Context,
	clients *Clients,
) (v1alpha1.GitOpsEngine, error) {
	clientset, err := clients.kubernetesClient()
	if err != nil {
		// If we can't get cluster config, we can't detect the GitOps engine.
		return v1alpha1.GitOpsEngineNone, ErrNoGitOpsEngine
	}

	if namespaceExists(ctx, clientset, "flux-system") {
		return v1alpha1.GitOpsEngineFlux, nil
	}

	if namespaceExists(ctx, clientset, "argocd") {
		return v1alpha1.GitOpsEngineArgoCD, nil
	}

	return v1alpha1.GitOpsEngineNone, ErrNoGitOpsEngine
}

// namespaceExists reports whether the named namespace is present in the cluster.
func namespaceExists(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
) bool {
	_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})

	return err == nil
}
