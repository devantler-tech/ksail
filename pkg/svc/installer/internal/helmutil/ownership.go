package helmutil

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
)

// GitOps controller label keys used to detect ownership of Helm release Secrets.
const (
	// FluxNameLabel is the label key set by the Flux helm-controller on
	// Helm release Secrets it manages.
	FluxNameLabel = "helm.toolkit.fluxcd.io/name"
	// ArgoCDManagedByLabel is the label key set by ArgoCD on resources it
	// manages. Present on Helm release Secrets when ArgoCD manages charts
	// through its Helm integration.
	ArgoCDManagedByLabel = "argocd.argoproj.io/managed-by"
)

// IsGitOpsManaged inspects Helm release Secret labels for known GitOps
// controller ownership markers. It returns the controller name and true when
// the release is managed externally; ("", false) otherwise.
func IsGitOpsManaged(labels map[string]string) (string, bool) {
	if len(labels) == 0 {
		return "", false
	}

	if _, ok := labels[FluxNameLabel]; ok {
		return "Flux", true
	}

	if _, ok := labels[ArgoCDManagedByLabel]; ok {
		return "ArgoCD", true
	}

	return "", false
}

// SkipIfGitOpsManaged reports whether a component's Helm lifecycle mutation
// should be skipped because the release Secret is already owned by a GitOps
// controller (Flux or ArgoCD). It queries the release storage labels, tolerating
// [helm.ErrNoReleaseStorage] (no release yet), and when ownership is detected
// it prints a skip notice to stderr and returns true.
//
// name identifies the component in the skip message and error wrapping (e.g.
// "cilium", "flux-operator"); releaseName and namespace identify the Helm
// release Secret to inspect. This single-sources the ownership-skip sequence
// shared by Base.Install, the CNI installers, the Flux installer, and the AWS
// Load Balancer Controller's guarded uninstall path.
func SkipIfGitOpsManaged(
	ctx context.Context,
	client helm.Interface,
	name, releaseName, namespace string,
) (bool, error) {
	labels, err := client.GetReleaseStorageLabels(ctx, releaseName, namespace)
	if err != nil && !errors.Is(err, helm.ErrNoReleaseStorage) {
		return false, fmt.Errorf("check release ownership for %s: %w", name, err)
	}

	controller, managed := IsGitOpsManaged(labels)
	if !managed {
		return false, nil
	}

	fmt.Fprintf(
		os.Stderr,
		"%s: skipping KSail-managed Helm change — release %q is managed by %s\n",
		name,
		releaseName,
		controller,
	)

	return true, nil
}
