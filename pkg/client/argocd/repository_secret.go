package argocd

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	argoCDNamespace = "argocd"
	//nolint:gosec // G101: false positive - this is a Kubernetes secret name, not a credential
	repositorySecretName = "ksail-local-registry-repo"
)

// repositorySecretOptions contains options for building the ArgoCD repository secret.
type repositorySecretOptions struct {
	repositoryURL string
	username      string
	password      string
	insecure      bool
}

func buildRepositorySecret(opts repositorySecretOptions) *corev1.Secret {
	data := map[string]string{
		"type": "oci",
		"url":  opts.repositoryURL,
	}

	// Add credentials if provided
	if opts.username != "" {
		data["username"] = opts.username
	}
	if opts.password != "" {
		data["password"] = opts.password
	}

	// Only set insecureOCIForceHttp for local registries (insecure mode)
	if opts.insecure {
		data["insecureOCIForceHttp"] = "true"
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      repositorySecretName,
			Namespace: argoCDNamespace,
			Labels:    map[string]string{"argocd.argoproj.io/secret-type": "repository"},
		},
		StringData: data,
	}
}
