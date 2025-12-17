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

func buildRepositorySecret(repositoryURL string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      repositorySecretName,
			Namespace: argoCDNamespace,
			Labels:    map[string]string{"argocd.argoproj.io/secret-type": "repository"},
		},
		StringData: map[string]string{
			"type":                 "oci",
			"url":                  repositoryURL,
			"insecureOCIForceHttp": "true",
		},
	}
}
