package kyvernoinstaller

import "k8s.io/client-go/kubernetes"

// SetNewClientsetFn overrides the clientset factory for testing.
// Returns a cleanup function that restores the original factory.
func SetNewClientsetFn(
	fn func(kubeconfig, kubecontext string) (kubernetes.Interface, error),
) func() {
	original := newClientsetFn
	newClientsetFn = fn

	return func() { newClientsetFn = original }
}
