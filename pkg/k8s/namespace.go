package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// podNamespaceEnvVar is the downward-API environment variable deployments set
	// with the pod's own namespace.
	podNamespaceEnvVar = "POD_NAMESPACE"
	// serviceAccountNamespaceFile is the projected file holding the pod's
	// namespace; the fallback when the downward-API env var is not set.
	serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// InClusterNamespace resolves the namespace the current process's pod runs in:
// the POD_NAMESPACE downward-API env var, falling back to the projected
// ServiceAccount namespace file. It returns "" when neither is available
// (running outside a cluster); callers pick their own default.
func InClusterNamespace() string {
	if namespace := os.Getenv(podNamespaceEnvVar); namespace != "" {
		return namespace
	}

	data, err := os.ReadFile(serviceAccountNamespaceFile)
	if err == nil {
		if namespace := strings.TrimSpace(string(data)); namespace != "" {
			return namespace
		}
	}

	return ""
}

// PSSLabels returns the Pod Security Standards admission labels
// (pod-security.kubernetes.io/{enforce,audit,warn}) for the given level.
// Returns nil when level is empty.
func PSSLabels(level string) map[string]string {
	if level == "" {
		return nil
	}

	return map[string]string{
		"pod-security.kubernetes.io/enforce": level,
		"pod-security.kubernetes.io/audit":   level,
		"pod-security.kubernetes.io/warn":    level,
	}
}

// pssLabels returns the PodSecurity Standard labels that grant "privileged" access.
// Talos (and other distributions) enforces PSS by default, so namespaces that
// run pods requiring elevated privileges (host networking, NET_ADMIN, etc.)
// must carry these labels.
func pssLabels() map[string]string {
	return PSSLabels("privileged")
}

// EnsurePrivilegedNamespace creates the given namespace with PodSecurity
// Standard "privileged" labels, or updates an existing namespace to add them.
func EnsurePrivilegedNamespace(
	ctx context.Context,
	clientset kubernetes.Interface,
	name string,
) error {
	namespace, err := clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: pssLabels(),
				},
			}

			_, err = clientset.CoreV1().Namespaces().Create(ctx, newNS, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("create namespace: %w", err)
			}

			return nil
		}

		return fmt.Errorf("get namespace: %w", err)
	}

	// Namespace exists — ensure PSS labels are set.
	if namespace.Labels == nil {
		namespace.Labels = make(map[string]string)
	}

	updated := false

	for k, v := range pssLabels() {
		if namespace.Labels[k] != v {
			namespace.Labels[k] = v
			updated = true
		}
	}

	if updated {
		_, err = clientset.CoreV1().Namespaces().Update(ctx, namespace, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update namespace labels: %w", err)
		}
	}

	return nil
}
