package helmutil

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
