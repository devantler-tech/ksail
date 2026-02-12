package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// pssLabels returns the PodSecurity Standard labels that grant "privileged" access.
// Talos (and other distributions) enforces PSS by default, so namespaces that
// run pods requiring elevated privileges (host networking, NET_ADMIN, etc.)
// must carry these labels.
func pssLabels() map[string]string {
	return map[string]string{
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
	}
}

// EnsurePrivilegedNamespace creates the given namespace with PodSecurity
// Standard "privileged" labels, or patches an existing namespace to add them.
func EnsurePrivilegedNamespace(
	ctx context.Context,
	clientset *kubernetes.Clientset,
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
